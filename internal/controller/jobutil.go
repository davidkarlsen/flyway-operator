package controller

import (
	"fmt"
	"path"
	"slices"
	"strconv"

	"github.com/caitlinelfring/go-env-default"
	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/samber/lo"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	defaultFlywayImage = "docker.io/flyway/flyway:13"
	envNameFlywayImage = "FLYWAY_IMAGE"
	// sqlMountPath is where the migration source image is mounted, read-only, in the Flyway container.
	sqlMountPath = "/flyway/migration-source"
)

func jobIsCurrent(job *batchv1.Job, migration *flywayv1alpha1.Migration) bool {
	return job.Annotations[flywayv1alpha1.Generation] == migration.GenerationAsString()
}

// from https://github.com/kubernetes/kubernetes/blob/v1.28.1/pkg/controller/job/utils.go
// IsJobFinished checks whether the given Job has finished execution.
// It does not discriminate between successful and failed terminations.
type jobStatus int

const (
	jobRunning jobStatus = iota
	jobSucceeded
	jobFailed
)

// jobState reads the Job's terminal conditions; on failure it also returns the Job's reason and message.
func jobState(job *batchv1.Job) (jobStatus, string) {
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return jobSucceeded, ""
		case batchv1.JobFailed:
			return jobFailed, fmt.Sprintf("%s: %s", c.Reason, c.Message)
		}
	}
	return jobRunning, ""
}

// jobAttempt returns the attempt annotation of the Job, 1 when absent (Jobs created before the annotation existed).
func jobAttempt(job *batchv1.Job) int32 {
	attempt, err := strconv.ParseInt(job.Annotations[flywayv1alpha1.Attempt], 10, 32)
	if err != nil || attempt < 1 {
		return 1
	}
	return int32(attempt)
}

// setConditions sets Applied=False, Failed=False and the given Progressing status, all with one reason and message.
func setConditions(m *flywayv1alpha1.Migration, reason, message string, progressing, failed metav1.ConditionStatus) {
	setAll(m, reason, message, metav1.ConditionFalse, progressing, failed)
}

func setApplied(m *flywayv1alpha1.Migration, message string) {
	setAll(m, flywayv1alpha1.ReasonJobSucceeded, message, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
}

func setFailed(m *flywayv1alpha1.Migration, message string) {
	setAll(m, flywayv1alpha1.ReasonRetryLimitExceeded, message, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
}

func setAll(m *flywayv1alpha1.Migration, reason, message string, applied, progressing, failed metav1.ConditionStatus) {
	m.Status.ObservedGeneration = m.Generation
	for condType, status := range map[string]metav1.ConditionStatus{
		flywayv1alpha1.ConditionApplied:     applied,
		flywayv1alpha1.ConditionProgressing: progressing,
		flywayv1alpha1.ConditionFailed:      failed,
	} {
		meta.SetStatusCondition(&m.Status.Conditions, metav1.Condition{
			Type:               condType,
			Status:             status,
			ObservedGeneration: m.Generation,
			Reason:             reason,
			Message:            message,
		})
	}
}

func getFlywayImage(migration *flywayv1alpha1.Migration) string {
	image, _ := lo.Coalesce(migration.Spec.FlywayConfiguration.FlywayImage, env.GetDefault(envNameFlywayImage, defaultFlywayImage))
	return image
}

func getFlywayArgs(migration *flywayv1alpha1.Migration) []string {
	args := slices.Clone(migration.Spec.FlywayConfiguration.Commands)
	args = append(args, "-outputType=json")

	jdbcProps := lo.MapToSlice(migration.Spec.FlywayConfiguration.JdbcProperties, func(key string, value string) string {
		return fmt.Sprintf("-environments.default.jdbcProperties.%s=%s", key, value)
	})
	args = append(args, jdbcProps...)

	return args
}

// sqlLocation is the Flyway location of the SQL files: spec.migrationSource.path inside the mounted source image.
func sqlLocation(migration *flywayv1alpha1.Migration) string {
	return "filesystem:" + path.Join(sqlMountPath, migration.Spec.MigrationSource.SqlPath)
}

// hasSourceImageVolume reports whether the Job still carries the migration source image volume.
// API servers with the ImageVolume feature gate off silently drop it on create.
func hasSourceImageVolume(job *batchv1.Job, migration *flywayv1alpha1.Migration) bool {
	return slices.ContainsFunc(job.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
		return v.Name == sqlVolumeName && v.Image != nil && v.Image.Reference == migration.Spec.MigrationSource.ImageRef
	})
}

func createJobSpec(migration *flywayv1alpha1.Migration, attempt int32) *batchv1.Job {
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
		{
			Name:  "FLYWAY_LOCATIONS",
			Value: sqlLocation(migration),
		},
		{
			// without it Flyway exits 0 without running anything when the location does not exist
			Name:  "FLYWAY_FAIL_ON_MISSING_LOCATIONS",
			Value: "true",
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
			Annotations: map[string]string{
				flywayv1alpha1.Generation: migration.GenerationAsString(),
				flywayv1alpha1.Attempt:    strconv.Itoa(int(attempt)),
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To[int32](2),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "flyway",
							Image:           getFlywayImage(migration),
							ImagePullPolicy: corev1.PullAlways,
							Args:            getFlywayArgs(migration),
							Env:             envVars,
							VolumeMounts: append([]corev1.VolumeMount{
								{
									Name:      sqlVolumeName,
									MountPath: sqlMountPath,
									ReadOnly:  true,
								},
							}, migration.Spec.FlywayConfiguration.VolumeMounts...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: sqlVolumeName,
							VolumeSource: corev1.VolumeSource{
								Image: &corev1.ImageVolumeSource{
									Reference:  migration.Spec.MigrationSource.ImageRef,
									PullPolicy: corev1.PullAlways,
								},
							},
						},
					}, migration.Spec.FlywayConfiguration.Volumes...),
					ImagePullSecrets: migration.Spec.MigrationSource.ImagePullSecrets,
					RestartPolicy:    corev1.RestartPolicyNever,
				},
			},
		},
	}

	return job
}
