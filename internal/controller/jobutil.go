package controller

import (
	"fmt"
	"strconv"

	"github.com/caitlinelfring/go-env-default"
	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/samber/lo"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	eq "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

const (
	defaultFlywayImage = "docker.io/flyway/flyway:9"
	envNameFlywayImage = "FLYWAY_IMAGE"
)

func jobsAreEqual(first *batchv1.Job, second *batchv1.Job) bool {
	return first != nil && second != nil &&
		eq.Semantic.DeepEqual(first.Spec.Template.Spec.InitContainers[0].Image, second.Spec.Template.Spec.InitContainers[0].Image)
}

// from https://github.com/kubernetes/kubernetes/blob/v1.28.1/pkg/controller/job/utils.go
// IsJobFinished checks whether the given Job has finished execution.
// It does not discriminate between successful and failed terminations.
func isJobFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func hasFailed(job *batchv1.Job) bool {
	return job.Status.Failed > 0
}

func hasSucceeded(job *batchv1.Job) bool {
	return job.Status.Succeeded > 0
}

func getFlywayImage(migration *flywayv1alpha1.Migration) string {
	image, _ := lo.Coalesce(migration.Spec.FlywayConfiguration.FlywayImage, env.GetDefault(envNameFlywayImage, defaultFlywayImage))
	return image
}

func getFlywayArgs(migration *flywayv1alpha1.Migration) []string {
	args := migration.Spec.FlywayConfiguration.Commands
	args = append(args, "-outputType=json")

	return args
}

func createJobSpec(migration *flywayv1alpha1.Migration) *batchv1.Job {
	const targetPath = "/mnt/target/"
	envVars := []corev1.EnvVar{
		{
			Name:  "FLYWAY_USER",
			Value: migration.Spec.Database.Username,
		},
		{
			Name: "FLYWAY_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &(migration.Spec.Database).Credentials,
			},
		},
		{
			Name:  "FLYWAY_URL",
			Value: migration.Spec.Database.JdbcUrl,
		},
		{
			Name:  "FLYWAY_ENCODING",
			Value: migration.Spec.MigrationSource.Encoding,
		},
	}

	if migration.Spec.FlywayConfiguration.BaselineOnMigrate != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "FLYWAY_BASELINE_ON_MIGRATE",
			Value: strconv.FormatBool(*migration.Spec.FlywayConfiguration.BaselineOnMigrate),
		})
	}

	if migration.Spec.FlywayConfiguration.DefaultSchema != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "FLYWAY_DEFAULT_SCHEMA",
			Value: *migration.Spec.FlywayConfiguration.DefaultSchema,
		})
	}
	envVars = append(envVars, migration.Spec.FlywayConfiguration.EnvVars...)
	envVars = append(envVars, migration.Spec.MigrationSource.GetPlaceholdersAsEnvVars()...)

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: batchv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      migration.Name,
			Namespace: migration.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "flyway-operator",
				"app.kubernetes.io/name":       "flyway",
				"app.kubernetes.io/instance":   migration.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: pointer.Int32(2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            "copy-sql",
							Image:           migration.Spec.MigrationSource.ImageRef,
							ImagePullPolicy: corev1.PullAlways,
							Command:         []string{"sh", "-c"},
							Args:            []string{fmt.Sprintf("cd %s && cp -rp * %s", migration.Spec.MigrationSource.SqlPath, targetPath)},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      sqlVolumeName,
									MountPath: targetPath,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:            "flyway",
							Image:           getFlywayImage(migration),
							ImagePullPolicy: corev1.PullAlways,
							Args:            getFlywayArgs(migration),
							Env:             envVars,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      sqlVolumeName,
									MountPath: "/flyway/sql",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: sqlVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					ImagePullSecrets: migration.Spec.MigrationSource.ImagePullSecrets,
					RestartPolicy:    corev1.RestartPolicyNever,
				},
			},
		},
	}

	return job
}
