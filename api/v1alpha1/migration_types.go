/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"
	"strconv"
	"time"

	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	Prefix     = "flyway-operator.davidkarlsen.com"
	Generation = Prefix + "/" + "generation"
	paused     = Prefix + "/" + "paused"
	// Attempt is the annotation on a Job holding which attempt (1-based) for its generation it is.
	Attempt = Prefix + "/" + "attempt"
	// MigrationLabel is the label on a Job and its pod template holding the name of the Migration.
	MigrationLabel = Prefix + "/" + "migration"

	// DefaultRetryLimit is used when spec.retryLimit is not set.
	DefaultRetryLimit int32 = 2
	// DefaultActiveDeadline is used when spec.activeDeadline is not set.
	DefaultActiveDeadline = time.Hour
)

// Condition types reported on MigrationStatus.Conditions.
// Every condition carries the observedGeneration it was computed for.
const (
	// ConditionApplied is True when the Job for the current generation succeeded.
	ConditionApplied = "Applied"
	// ConditionProgressing is True while a Job for the current generation is created, running or being retried.
	ConditionProgressing = "Progressing"
	// ConditionFailed is True when the Job for the current generation failed and retryLimit is exhausted,
	// with reason TimedOut when that last attempt exceeded spec.activeDeadline, else RetryLimitExceeded;
	// or with reason ImageVolumeUnsupported when the API server dropped the source image volume from the Job.
	ConditionFailed = "Failed"
)

// Reasons set on the Applied, Progressing and Failed conditions.
const (
	ReasonJobCreated         = "JobCreated"
	ReasonJobRunning         = "JobRunning"
	ReasonJobSucceeded       = "JobSucceeded"
	ReasonRetrying           = "Retrying"
	ReasonRetryLimitExceeded = "RetryLimitExceeded"
	// ReasonTimedOut replaces RetryLimitExceeded when the last attempt's Job exceeded spec.activeDeadline.
	// It is also the reason of the Warning event emitted then.
	ReasonTimedOut            = "TimedOut"
	ReasonPreviousJobRunning  = "PreviousJobRunning"
	ReasonPreviousJobReplaced = "PreviousJobReplaced"
	// ReasonImageVolumeUnsupported means the API server dropped the source image volume from the Job.
	ReasonImageVolumeUnsupported = "ImageVolumeUnsupported"
)

// MigrationStatus defines the observed state of Migration
type MigrationStatus struct {
	// The metadata.generation the status was computed for.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Number of Jobs started for observedGeneration.
	// +optional
	Attempts int32 `json:"attempts,omitempty"`

	// The migration source image (spec.migrationSource.imageRef) of the last Job that succeeded.
	// This is the image the SQL files came from, not the schema version in the database.
	// Kept when a later run fails or is in progress.
	// +optional
	LastAppliedImageRef string `json:"lastAppliedImageRef,omitempty"`

	// The metadata.generation of the Migration whose Job set lastAppliedImageRef.
	// +optional
	LastAppliedGeneration int64 `json:"lastAppliedGeneration,omitempty"`

	// The database's Flyway schema version as reported by the last Job that succeeded: the last info or migrate
	// result in its JSON output. Kept when a later run fails, is in progress, or reports no version.
	// +optional
	// +kubebuilder:validation:MaxLength=64
	SchemaVersion string `json:"schemaVersion,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (m *Migration) IsPaused() bool {
	filtered := lo.PickBy(m.Annotations, func(key, value string) bool {
		return key == paused && value == strconv.FormatBool(true)
	})
	return len(filtered) > 0
}

func (m *Migration) GenerationAsString() string {
	return strconv.Itoa(int(m.Generation))
}

// GetRetryLimit returns spec.retryLimit, or DefaultRetryLimit when unset.
func (m *Migration) GetRetryLimit() int32 {
	if m.Spec.RetryLimit == nil {
		return DefaultRetryLimit
	}
	return *m.Spec.RetryLimit
}

// GetActiveDeadline returns spec.activeDeadline, or DefaultActiveDeadline when unset.
func (m *Migration) GetActiveDeadline() time.Duration {
	if m.Spec.ActiveDeadline == nil {
		return DefaultActiveDeadline
	}
	return m.Spec.ActiveDeadline.Duration
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Schema Version",type=string,JSONPath=`.status.schemaVersion`
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
//+kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 63",message="metadata.name must be at most 63 characters (it is used as the Job name and as a pod label value)"

// Migration is the Schema for the migrations API
type Migration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   MigrationSpec   `json:"spec,omitempty"`
	Status MigrationStatus `json:"status,omitempty"`
}

// MigrationSpec defines the desired state of Migration
type MigrationSpec struct {
	// settings for database connection
	// +kubebuilder:validation:Required
	Database Database `json:"database"`

	// settings for flyway
	FlywayConfiguration FlywayConfiguration `json:"flywayConfiguration"`

	// settings defining the SQL migrations
	// +kubebuilder:validation:Required
	MigrationSource MigrationSource `json:"migrationSource"`

	// How many times the operator re-creates a failed Job for the same generation.
	// After that the Migration reports Failed=True and no new Job is started until the spec changes.
	// Each Job also retries its pod twice (Job backoffLimit 2) before it counts as failed.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=2
	RetryLimit *int32 `json:"retryLimit,omitempty"`

	// Maximum run time per Job attempt, e.g. "30m", set as the Job's activeDeadlineSeconds (rounded up to whole seconds).
	// It includes image pulls and the Job's own pod retries.
	// A Job that exceeds it fails with reason DeadlineExceeded and counts against retryLimit;
	// when the last attempt times out the Migration reports Failed=True with reason TimedOut.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="1h"
	// +kubebuilder:validation:XValidation:rule="duration(self) >= duration('1s') && duration(self) <= duration('8760h')",message="must be a duration such as 30m, at least 1s and at most 8760h"
	ActiveDeadline *metav1.Duration `json:"activeDeadline,omitempty"`

	// Labels and annotations to set on the Job. Nothing is copied from the Migration's own metadata.
	// A change creates a new Job, re-running the migration.
	// +kubebuilder:validation:Optional
	JobMetadata *ObjectMetadata `json:"jobMetadata,omitempty"`

	// Labels and annotations to set on the Job's pod template, e.g. sidecar.istio.io/inject: "false".
	// Nothing is copied from the Migration's own metadata.
	// A change creates a new Job, re-running the migration.
	// +kubebuilder:validation:Optional
	PodMetadata *ObjectMetadata `json:"podMetadata,omitempty"`
}

// ObjectMetadata holds labels and annotations the operator sets on an object it creates.
// The operator's own keys are reserved: keys under flyway-operator.davidkarlsen.com/ and batch.kubernetes.io/,
// controller-uid, job-name and app.kubernetes.io/managed-by.
type ObjectMetadata struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxProperties=64
	// +kubebuilder:validation:XValidation:rule="self.all(k, !k.startsWith('flyway-operator.davidkarlsen.com/') && !k.startsWith('batch.kubernetes.io/') && !(k in ['controller-uid', 'job-name', 'app.kubernetes.io/managed-by']))",message="must not contain a reserved key: flyway-operator.davidkarlsen.com/*, batch.kubernetes.io/*, controller-uid, job-name or app.kubernetes.io/managed-by"
	// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) <= 317 && k.matches('^([a-z0-9]([-a-z0-9]*[a-z0-9])?([.][a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$') && (!k.contains('/') || k.indexOf('/') <= 253))",message="keys must be valid label keys: an optional DNS subdomain prefix of at most 253 characters and '/', then a name of at most 63 alphanumeric characters, '-', '_' or '.', starting and ending with an alphanumeric character"
	Labels map[string]LabelValue `json:"labels,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxProperties=64
	// +kubebuilder:validation:XValidation:rule="self.all(k, !k.startsWith('flyway-operator.davidkarlsen.com/') && !k.startsWith('batch.kubernetes.io/') && !(k in ['controller-uid', 'job-name', 'app.kubernetes.io/managed-by']))",message="must not contain a reserved key: flyway-operator.davidkarlsen.com/*, batch.kubernetes.io/*, controller-uid, job-name or app.kubernetes.io/managed-by"
	// +kubebuilder:validation:XValidation:rule="self.all(k, size(k) <= 317 && k.matches('^([A-Za-z0-9]([-A-Za-z0-9]*[A-Za-z0-9])?([.][A-Za-z0-9]([-A-Za-z0-9]*[A-Za-z0-9])?)*/)?[A-Za-z0-9]([-A-Za-z0-9_.]{0,61}[A-Za-z0-9])?$') && (!k.contains('/') || k.indexOf('/') <= 253))",message="keys must be valid annotation keys: an optional DNS subdomain prefix of at most 253 characters and '/', then a name of at most 63 alphanumeric characters, '-', '_' or '.', starting and ending with an alphanumeric character"
	Annotations map[string]string `json:"annotations,omitempty"`
}

// LabelValue is a Kubernetes label value: empty, or at most 63 alphanumeric characters, '-', '_' or '.',
// starting and ending with an alphanumeric character.
// +kubebuilder:validation:MaxLength=63
// +kubebuilder:validation:Pattern=`^(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?$`
type LabelValue string

// GetLabels returns the labels as plain strings, or nil when o or its labels are nil.
func (o *ObjectMetadata) GetLabels() map[string]string {
	if o == nil || o.Labels == nil {
		return nil
	}
	labels := make(map[string]string, len(o.Labels))
	for k, v := range o.Labels {
		labels[k] = string(v)
	}
	return labels
}

// GetAnnotations returns the annotations, or nil when o is nil.
func (o *ObjectMetadata) GetAnnotations() map[string]string {
	if o == nil {
		return nil
	}
	return o.Annotations
}

// Database defines the database-settings
type Database struct {
	// username for connecting to database. Passed as FLYWAY_USER; unset leaves it out, for authentication that
	// needs no user name (for example ActiveDirectoryDefault).
	// +kubebuilder:validation:Optional
	Username string `json:"username,omitempty"`

	// reference to a secret containing the password for connecting to database. Passed as FLYWAY_PASSWORD.
	// Leave it unset for passwordless login, where the JDBC driver authenticates through jdbcUrl and
	// jdbcProperties and the pod's workload identity (see spec.flywayConfiguration.serviceAccountName).
	// +kubebuilder:validation:Optional
	Credentials *v1.SecretKeySelector `json:"credentials,omitempty"`

	// the jdbcUrl to connect to database
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^jdbc:.*`
	JdbcUrl string `json:"jdbcUrl"`
}

// GetCredentials returns the reference to the password secret, or the zero reference when credentials is unset.
func (r *Migration) GetCredentials() v1.SecretReference {
	if r.Spec.Database.Credentials == nil {
		return v1.SecretReference{}
	}
	return v1.SecretReference{
		Name:      r.Spec.Database.Credentials.Name,
		Namespace: r.Namespace,
	}
}

type FlywayConfiguration struct {
	// Reference to the flyway image to use.
	// +kubebuilder:validation:Optional
	FlywayImage string `json:"flywayImage"`

	// The flyway actions to apply, like "info", "migrate"
	// See https://documentation.red-gate.com/fd/commands-184127446.html
	// +kubebuilder:default={"info", "migrate", "info"}
	Commands []string `json:"commands"`

	// The default flyway schema to use.
	// See https://documentation.red-gate.com/fd/default-schema-184127496.html
	// +kubebuilder:validation:Optional
	DefaultSchema *string `json:"defaultSchema"`

	// Base-line on migrate.
	// See https://documentation.red-gate.com/fd/baseline-on-migrate-224919695.html
	// +kubebuilder:validation:Optional
	BaselineOnMigrate *bool `json:"baselineOnMigrate"`

	// Arbitrary entries to set as env-vars to Flyway migration job.
	// +kubebuilder:validation:Optional
	EnvVars []v1.EnvVar `json:"envVars"`

	// jdbcProperties to pass to the execution.
	// See https://documentation.red-gate.com/fd/environment-jdbc-properties-namespace-277578928.html
	// +kubebuilder:validation:Optional
	JdbcProperties map[string]string `json:"jdbcProperties,omitempty"`

	// Volumes to make available to the migration job.
	// +kubebuilder:validation:Optional
	Volumes []v1.Volume `json:"volumes,omitempty"`

	// Volume mounts to mount into the migration job.
	// +kubebuilder:validation:Optional
	VolumeMounts []v1.VolumeMount `json:"volumeMounts,omitempty"`

	// Security context of the migration job's pod. Replaces the operator's restricted default
	// (runAsNonRoot, RuntimeDefault seccomp and, outside OpenShift, runAsUser and fsGroup 1001).
	// +kubebuilder:validation:Optional
	PodSecurityContext *v1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Security context of the migration job's flyway container. Replaces the operator's restricted
	// default (no privilege escalation, all capabilities dropped, read-only root filesystem).
	// +kubebuilder:validation:Optional
	SecurityContext *v1.SecurityContext `json:"securityContext,omitempty"`

	// Compute resources (requests and limits) of the migration job's flyway container. Unset by default.
	// Set it in namespaces with a ResourceQuota, which rejects pods without requests and limits.
	// claims is not supported: the operator defines no pod resourceClaims for it to refer to.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="!has(self.claims)",message="resources.claims is not supported: the operator does not define the pod's resourceClaims"
	Resources *v1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector of the migration job's pod.
	// +kubebuilder:validation:Optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations of the migration job's pod.
	// +kubebuilder:validation:Optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`

	// Affinity of the migration job's pod.
	// +kubebuilder:validation:Optional
	Affinity *v1.Affinity `json:"affinity,omitempty"`

	// PriorityClassName of the migration job's pod.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// ServiceAccountName the migration job's pod runs as. Defaults to the namespace's default service account.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// MigrationSource defines the source for the flyway-migrations.
type MigrationSource struct {

	// Reference to the image holding the SQLs to migrate.
	// The image is mounted read-only as an image volume, so it needs no shell or other tools and can be built FROM scratch.
	// +kubebuilder:validation:Required
	ImageRef string `json:"imageRef"`

	// Optional. Image-pull secret to pull the migration source
	// +kubebuilder:validation:Optional
	ImagePullSecrets []v1.LocalObjectReference `json:"ImagePullSecret"`

	// Path within the image to the SQLs for flyway
	// +kubebuilder:default="/sql"
	// +kubebuilder:validation:MaxLength=1024
	// +kubebuilder:validation:XValidation:rule="!self.matches('(^|/)[.][.](/|$)')",message="path must not contain '..'"
	SqlPath string `json:"path"`

	// The encoding of the SQL-files.
	// +kubebuilder:default="UTF-8"
	Encoding string `json:"encoding"`

	// Flyway placeholders, see: https://documentation.red-gate.com/fd/placeholders-configuration-184127475.html
	// These will be injected as env-vars with the required prefix.
	// +kubebuilder:validation:Optional
	Placeholders map[string]string `json:"placeholders"`
}

func (r *MigrationSource) GetPlaceholdersAsEnvVars() []v1.EnvVar {
	return lo.MapToSlice(r.Placeholders, func(key string, value string) v1.EnvVar {
		return v1.EnvVar{
			Name:  fmt.Sprintf("FLYWAY_PLACEHOLDERS_%s", key),
			Value: value,
		}
	})
}

//+kubebuilder:object:root=true

// MigrationList contains a list of Migration
type MigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Migration `json:"items"`
}
