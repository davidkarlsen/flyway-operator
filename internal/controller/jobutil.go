package controller

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
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
	// DefaultFlywayImage is the Flyway image used when neither spec.flywayConfiguration.flywayImage nor FLYWAY_IMAGE is set.
	DefaultFlywayImage = defaultFlywayImage
	// copySQLContainerName is the init container copying the SQL files out of the migration source image.
	copySQLContainerName = "copy-sql"
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

// jobSourceImage returns the migration source image of the Job: the image of the copy-sql init container,
// or else the reference of the SQL image volume. It returns "" when the Job has neither.
func jobSourceImage(job *batchv1.Job) string {
	podSpec := job.Spec.Template.Spec
	for _, c := range podSpec.InitContainers {
		if c.Name == copySQLContainerName {
			return c.Image
		}
	}
	for _, v := range podSpec.Volumes {
		if v.Name == sqlVolumeName && v.Image != nil {
			return v.Image.Reference
		}
	}
	return ""
}

// setLastApplied records the succeeded Job's source image and generation on the status.
// When either cannot be read from the Job, the status is left as is and false is returned.
func setLastApplied(m *flywayv1alpha1.Migration, job *batchv1.Job) bool {
	image := jobSourceImage(job)
	generation, err := strconv.ParseInt(job.Annotations[flywayv1alpha1.Generation], 10, 64)
	if image == "" || err != nil {
		return false
	}
	m.Status.LastAppliedImageRef = image
	m.Status.LastAppliedGeneration = generation
	return true
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
	args := migration.Spec.FlywayConfiguration.Commands
	args = append(args, "-outputType=json")

	jdbcProps := lo.MapToSlice(migration.Spec.FlywayConfiguration.JdbcProperties, func(key string, value string) string {
		return fmt.Sprintf("-environments.default.jdbcProperties.%s=%s", key, value)
	})
	args = append(args, jdbcProps...)

	return args
}

// operatorLabels returns the labels the operator sets on the Job and its pod template.
// They avoid app.kubernetes.io/name and instance, which application selectors (Helm release name) match on.
func operatorLabels(migration *flywayv1alpha1.Migration) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "flyway-operator",
		flywayv1alpha1.MigrationLabel:  migrationLabelValue(migration.Name),
	}
}

// migrationLabelValue returns name, or for names longer than a label value's 63 characters,
// its first 54 characters, "-" and the first 8 hex characters of the name's SHA-256.
func migrationLabelValue(name string) string {
	const maxLabelValue, keep = 63, 54
	if len(name) <= maxLabelValue {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return name[:keep] + "-" + hex.EncodeToString(sum[:])[:8]
}

// withOperatorKeys returns the user-set entries with the operator's entries set over them, so the operator's keys win.
func withOperatorKeys(user, operator map[string]string) map[string]string {
	out := make(map[string]string, len(user)+len(operator))
	maps.Copy(out, user)
	maps.Copy(out, operator)
	return out
}

func createJobSpec(migration *flywayv1alpha1.Migration, attempt int32) *batchv1.Job {
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
			Labels:    withOperatorKeys(migration.Spec.JobMetadata.GetLabels(), operatorLabels(migration)),
			Annotations: withOperatorKeys(migration.Spec.JobMetadata.GetAnnotations(), map[string]string{
				flywayv1alpha1.Generation: migration.GenerationAsString(),
				flywayv1alpha1.Attempt:    strconv.Itoa(int(attempt)),
			}),
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To[int32](2),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      withOperatorKeys(migration.Spec.PodMetadata.GetLabels(), operatorLabels(migration)),
					Annotations: maps.Clone(migration.Spec.PodMetadata.GetAnnotations()),
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            copySQLContainerName,
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
							VolumeMounts: append([]corev1.VolumeMount{
								{
									Name:      sqlVolumeName,
									MountPath: "/flyway/sql",
								},
							}, migration.Spec.FlywayConfiguration.VolumeMounts...),
						},
					},
					Volumes: append([]corev1.Volume{
						{
							Name: sqlVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
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
