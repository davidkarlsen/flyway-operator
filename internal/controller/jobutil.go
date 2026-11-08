package controller

import (
	"errors"
	"fmt"
	"maps"
	"math"
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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	defaultFlywayImage = "docker.io/flyway/flyway:13"
	envNameFlywayImage = "FLYWAY_IMAGE"
	// DefaultFlywayImage is the Flyway image used when neither spec.flywayConfiguration.flywayImage nor FLYWAY_IMAGE is set.
	DefaultFlywayImage = defaultFlywayImage
	// copySQLContainerName is the init container that Jobs created by operator versions before 0.6.0 used to copy the SQL.
	copySQLContainerName = "copy-sql"
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

// jobState reads the Job's terminal conditions; on failure it also returns the Job condition's reason,
// and its reason and message as one string.
func jobState(job *batchv1.Job) (state jobStatus, reason, failure string) {
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			return jobSucceeded, "", ""
		case batchv1.JobFailed:
			return jobFailed, c.Reason, fmt.Sprintf("%s: %s", c.Reason, c.Message)
		}
	}
	return jobRunning, "", ""
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

// setFailed sets Failed=True with reason RetryLimitExceeded or TimedOut.
func setFailed(m *flywayv1alpha1.Migration, reason, message string) {
	setAll(m, reason, message, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
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

// operatorLabels returns the labels the operator sets on the Job and its pod template.
// They avoid app.kubernetes.io/name and instance, which application selectors (Helm release name) match on.
func operatorLabels(migration *flywayv1alpha1.Migration) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": "flyway-operator",
		flywayv1alpha1.MigrationLabel:  migration.Name,
	}
}

// withOperatorKeys returns the user-set entries with the operator's entries set over them, so the operator's keys win.
func withOperatorKeys(user, operator map[string]string) map[string]string {
	out := make(map[string]string, len(user)+len(operator))
	maps.Copy(out, user)
	maps.Copy(out, operator)
	return out
}

// sqlLocation is the Flyway location of the SQL files: spec.migrationSource.path inside the mounted source image.
func sqlLocation(migration *flywayv1alpha1.Migration) string {
	return "filesystem:" + path.Join(sqlMountPath, migration.Spec.MigrationSource.SqlPath)
}

// hasSourceImageVolume reports whether the Job still carries the migration source image volume.
// API servers with the ImageVolume feature gate off silently drop it on create. The reference is not compared:
// an admission webhook may rewrite it to a digest or a mirror.
func hasSourceImageVolume(job *batchv1.Job) bool {
	return slices.ContainsFunc(job.Spec.Template.Spec.Volumes, func(v corev1.Volume) bool {
		return v.Name == sqlVolumeName && v.Image != nil
	})
}

// copyTolerations deep-copies ts, keeping nil as nil.
func copyTolerations(ts []corev1.Toleration) []corev1.Toleration {
	if ts == nil {
		return nil
	}
	out := make([]corev1.Toleration, len(ts))
	for i := range ts {
		ts[i].DeepCopyInto(&out[i])
	}
	return out
}

// resourcesOrEmpty deep-copies r, or returns empty requirements when unset.
func resourcesOrEmpty(r *corev1.ResourceRequirements) corev1.ResourceRequirements {
	if r == nil {
		return corev1.ResourceRequirements{}
	}
	return *r.DeepCopy()
}

func createJobSpec(migration *flywayv1alpha1.Migration, attempt int32) *batchv1.Job {
	config := migration.Spec.FlywayConfiguration
	var envVars []corev1.EnvVar
	if migration.Spec.Database.Username != "" {
		envVars = append(envVars, corev1.EnvVar{Name: "FLYWAY_USER", Value: migration.Spec.Database.Username})
	}
	// Without credentials there is no FLYWAY_PASSWORD: the driver authenticates through the URL and the pod's identity.
	if migration.Spec.Database.Credentials != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:      "FLYWAY_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: migration.Spec.Database.Credentials.DeepCopy()},
		})
	}
	envVars = append(envVars, []corev1.EnvVar{
		{
			Name:  "FLYWAY_URL",
			Value: migration.Spec.Database.JdbcUrl,
		},
		{
			Name:  "FLYWAY_ENCODING",
			Value: migration.Spec.MigrationSource.Encoding,
		},
	}...)
	for _, e := range []corev1.EnvVar{
		{Name: "FLYWAY_LOCATIONS", Value: sqlLocation(migration)},
		// without it Flyway exits 0 without running anything when the location does not exist
		{Name: "FLYWAY_FAIL_ON_MISSING_LOCATIONS", Value: "true"},
	} {
		userSet := slices.ContainsFunc(migration.Spec.FlywayConfiguration.EnvVars, func(u corev1.EnvVar) bool { return u.Name == e.Name })
		if !userSet {
			envVars = append(envVars, e)
		}
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
					Containers: []corev1.Container{
						{
							Name:            flywayContainerName,
							Image:           getFlywayImage(migration),
							ImagePullPolicy: corev1.PullAlways,
							Args:            getFlywayArgs(migration),
							Env:             envVars,
							Resources:       resourcesOrEmpty(config.Resources),
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
					ImagePullSecrets:   migration.Spec.MigrationSource.ImagePullSecrets,
					RestartPolicy:      corev1.RestartPolicyNever,
					NodeSelector:       maps.Clone(config.NodeSelector),
					Tolerations:        copyTolerations(config.Tolerations),
					Affinity:           config.Affinity.DeepCopy(),
					PriorityClassName:  config.PriorityClassName,
					ServiceAccountName: config.ServiceAccountName,
				},
			},
		},
	}

	job.Spec.ActiveDeadlineSeconds = ptr.To(int64(math.Ceil(migration.GetActiveDeadline().Seconds())))

	return job
}

const (
	// defaultRunAsUser is the UID (and fsGroup) of the Job's pod outside OpenShift: Kubernetes cannot verify
	// runAsNonRoot for an image without a non-root USER (the Flyway image has none) unless the pod sets one.
	// 1001 owns the Flyway launcher in the Flyway image.
	defaultRunAsUser int64 = 1001
	// tmpVolumeName is the writable /tmp of the Job's containers, whose root filesystem is read-only.
	tmpVolumeName = "flyway-operator-tmp"
	tmpPath       = "/tmp"
)

// IsOpenShift reports whether the cluster serves OpenShift's security.openshift.io API. A failed discovery of the
// group is an error, not "not OpenShift": guessing wrong would give the Job's pod a UID the SCC rejects.
func IsOpenShift(mapper meta.RESTMapper) (bool, error) {
	_, err := mapper.KindFor(schema.GroupVersionResource{Group: "security.openshift.io", Resource: "securitycontextconstraints"})
	switch {
	case err == nil:
		return true, nil
	case meta.IsNoMatchError(err) && !errors.As(err, new(*apiutil.ErrResourceDiscoveryFailed)):
		return false, nil
	default:
		return false, err
	}
}

// defaultPodSecurityContext returns the restricted pod security context. On OpenShift the UID and fsGroup are left
// to the SCC; elsewhere the pod runs as, and its volumes are owned by, defaultRunAsUser.
func defaultPodSecurityContext(openShift bool) *corev1.PodSecurityContext {
	sc := &corev1.PodSecurityContext{
		RunAsNonRoot:        ptr.To(true),
		FSGroupChangePolicy: ptr.To(corev1.FSGroupChangeOnRootMismatch),
		SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	if !openShift {
		sc.RunAsUser = ptr.To(defaultRunAsUser)
		sc.FSGroup = ptr.To(defaultRunAsUser)
	}
	return sc
}

// setSecurityContext makes the pod comply with the Pod Security Standard "restricted": non-root, RuntimeDefault
// seccomp, and every container without privilege escalation or capabilities and with a read-only root filesystem
// plus a writable emptyDir at /tmp. spec.flywayConfiguration.podSecurityContext and securityContext replace the
// pod's and the containers' defaults.
func setSecurityContext(podSpec *corev1.PodSpec, migration *flywayv1alpha1.Migration, openShift bool) {
	config := migration.Spec.FlywayConfiguration
	podSpec.SecurityContext = config.PodSecurityContext.DeepCopy()
	if podSpec.SecurityContext == nil {
		podSpec.SecurityContext = defaultPodSecurityContext(openShift)
	}
	if !lo.ContainsBy(podSpec.Volumes, func(v corev1.Volume) bool { return v.Name == tmpVolumeName }) {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name:         tmpVolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
	}
	for _, containers := range [][]corev1.Container{podSpec.InitContainers, podSpec.Containers} {
		for i := range containers {
			c := &containers[i]
			c.SecurityContext = config.SecurityContext.DeepCopy()
			if c.SecurityContext == nil {
				c.SecurityContext = &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
					ReadOnlyRootFilesystem:   ptr.To(true),
				}
			}
			if !lo.ContainsBy(c.VolumeMounts, func(m corev1.VolumeMount) bool { return path.Clean(m.MountPath) == tmpPath }) {
				c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: tmpVolumeName, MountPath: tmpPath})
			}
		}
	}
}
