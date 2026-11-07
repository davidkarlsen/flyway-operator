package controller

import (
	"errors"
	"maps"
	"path"
	"reflect"
	"strconv"
	"testing"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/samber/lo"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

func TestCreateJobSpecMetadata(t *testing.T) {
	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-app",
			Generation:  3,
			Labels:      map[string]string{"app": "my-app"},
			Annotations: map[string]string{"argocd.argoproj.io/tracking-id": "x"},
		},
		Spec: flywayv1alpha1.MigrationSpec{
			JobMetadata: &flywayv1alpha1.ObjectMetadata{
				Labels:      map[string]flywayv1alpha1.LabelValue{"team": "payments", "app.kubernetes.io/managed-by": "user"},
				Annotations: map[string]string{"cost-center": "42", flywayv1alpha1.Generation: "user"},
			},
			PodMetadata: &flywayv1alpha1.ObjectMetadata{
				Labels:      map[string]flywayv1alpha1.LabelValue{"netpol": "db", flywayv1alpha1.MigrationLabel: "user", "app.kubernetes.io/instance": "my-release"},
				Annotations: map[string]string{"sidecar.istio.io/inject": "false"},
			},
		},
	}
	operator := map[string]string{
		"app.kubernetes.io/managed-by": "flyway-operator",
		flywayv1alpha1.MigrationLabel:  "my-app",
	}

	job := createJobSpec(migration, 2)
	pod := job.Spec.Template.ObjectMeta

	// jobMetadata reaches only the Job, podMetadata only the pod template, the Migration's own metadata neither,
	// and the operator's keys win over user-set ones.
	wantJobLabels := map[string]string{"team": "payments"}
	maps.Copy(wantJobLabels, operator)
	if !maps.Equal(job.Labels, wantJobLabels) {
		t.Errorf("job labels = %v, want %v", job.Labels, wantJobLabels)
	}
	wantJobAnnotations := map[string]string{"cost-center": "42", flywayv1alpha1.Generation: "3", flywayv1alpha1.Attempt: "2"}
	if !maps.Equal(job.Annotations, wantJobAnnotations) {
		t.Errorf("job annotations = %v, want %v", job.Annotations, wantJobAnnotations)
	}
	wantPodLabels := map[string]string{"netpol": "db", "app.kubernetes.io/instance": "my-release"}
	maps.Copy(wantPodLabels, operator)
	if !maps.Equal(pod.Labels, wantPodLabels) {
		t.Errorf("pod template labels = %v, want %v", pod.Labels, wantPodLabels)
	}
	wantPodAnnotations := map[string]string{"sidecar.istio.io/inject": "false"}
	if !maps.Equal(pod.Annotations, wantPodAnnotations) {
		t.Errorf("pod template annotations = %v, want %v", pod.Annotations, wantPodAnnotations)
	}
}

func TestCreateJobSpecDoesNotAliasSpecMetadata(t *testing.T) {
	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
		Spec: flywayv1alpha1.MigrationSpec{
			JobMetadata: &flywayv1alpha1.ObjectMetadata{
				Labels:      map[string]flywayv1alpha1.LabelValue{"team": "payments"},
				Annotations: map[string]string{"owner": "payments"},
			},
			PodMetadata: &flywayv1alpha1.ObjectMetadata{
				Labels:      map[string]flywayv1alpha1.LabelValue{"netpol": "db"},
				Annotations: map[string]string{"sidecar.istio.io/inject": "false"},
			},
		},
	}
	want := migration.Spec.DeepCopy()

	job := createJobSpec(migration, 1)
	for _, m := range []map[string]string{job.Labels, job.Annotations, job.Spec.Template.Labels, job.Spec.Template.Annotations} {
		for k := range m {
			m[k] = "mutated"
		}
		m["added"] = "x"
	}

	if !equality.Semantic.DeepEqual(&migration.Spec, want) {
		t.Errorf("mutating the Job changed the Migration spec: got %+v, want %+v", migration.Spec, *want)
	}
}

func TestCreateJobSpecWithoutMetadata(t *testing.T) {
	migration := &flywayv1alpha1.Migration{ObjectMeta: metav1.ObjectMeta{Name: "my-app", Labels: map[string]string{"app": "my-app"}}}

	job := createJobSpec(migration, 1)

	want := map[string]string{
		"app.kubernetes.io/managed-by": "flyway-operator",
		flywayv1alpha1.MigrationLabel:  "my-app",
	}
	if !maps.Equal(job.Labels, want) {
		t.Errorf("job labels = %v, want %v", job.Labels, want)
	}
	if !maps.Equal(job.Spec.Template.Labels, want) {
		t.Errorf("pod template labels = %v, want %v", job.Spec.Template.Labels, want)
	}
	if len(job.Spec.Template.Annotations) != 0 {
		t.Errorf("pod template annotations = %v, want none", job.Spec.Template.Annotations)
	}
}

func TestJobSourceImage(t *testing.T) {
	podJob := func(spec corev1.PodSpec) *batchv1.Job {
		return &batchv1.Job{Spec: batchv1.JobSpec{Template: corev1.PodTemplateSpec{Spec: spec}}}
	}
	tests := []struct {
		name string
		job  *batchv1.Job
		want string
	}{
		{
			name: "copy-sql init container",
			job: podJob(corev1.PodSpec{InitContainers: []corev1.Container{
				{Name: "other", Image: "reg.io/other:1"},
				{Name: copySQLContainerName, Image: "reg.io/sql:1"},
			}}),
			want: "reg.io/sql:1",
		},
		{
			name: "image volume",
			job: podJob(corev1.PodSpec{Volumes: []corev1.Volume{
				{Name: "tmp", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "other", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "reg.io/other:2"}}},
				{Name: sqlVolumeName, VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "reg.io/sql:2"}}},
			}}),
			want: "reg.io/sql:2",
		},
		{
			name: "neither",
			job: podJob(corev1.PodSpec{
				InitContainers: []corev1.Container{{Name: "other", Image: "reg.io/other:1"}},
				Volumes: []corev1.Volume{
					{Name: sqlVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{Name: "other", VolumeSource: corev1.VolumeSource{Image: &corev1.ImageVolumeSource{Reference: "reg.io/other:2"}}},
				},
			}),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jobSourceImage(tt.job); got != tt.want {
				t.Errorf("jobSourceImage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCreateJobSpec(t *testing.T) {
	tests := []struct {
		name          string
		migration     flywayv1alpha1.Migration
		envAssertions map[string]string // expected env var name/value
	}{
		{
			name: "basic config",
			migration: flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					Database: flywayv1alpha1.Database{
						Username: "testuser",
						Credentials: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "db-secret"},
							Key:                  "password",
						},
						JdbcUrl: "jdbc:testurl",
					},
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{},
					MigrationSource: flywayv1alpha1.MigrationSource{
						Encoding: "UTF-8",
					},
				},
			},
			envAssertions: map[string]string{
				"FLYWAY_USER":     "testuser",
				"FLYWAY_URL":      "jdbc:testurl",
				"FLYWAY_ENCODING": "UTF-8",
			},
		},
		{
			name: "baseline and schema",
			migration: flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					Database: flywayv1alpha1.Database{
						Username: "user2",
						Credentials: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "secret2"},
							Key:                  "pass2",
						},
						JdbcUrl: "jdbc:url2",
					},
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
						BaselineOnMigrate: func() *bool { b := true; return &b }(),
						DefaultSchema:     func() *string { s := "myschema"; return &s }(),
					},
					MigrationSource: flywayv1alpha1.MigrationSource{
						Encoding: "ISO-8859-1",
					},
				},
			},
			envAssertions: map[string]string{
				"FLYWAY_USER":                "user2",
				"FLYWAY_URL":                 "jdbc:url2",
				"FLYWAY_ENCODING":            "ISO-8859-1",
				"FLYWAY_BASELINE_ON_MIGRATE": strconv.FormatBool(true),
				"FLYWAY_DEFAULT_SCHEMA":      "myschema",
			},
		},
		{
			name: "volume and volumemount",
			migration: flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					Database: flywayv1alpha1.Database{
						Username: "voluser",
						Credentials: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "vol-secret"},
							Key:                  "volpass",
						},
						JdbcUrl: "jdbc:volurl",
					},
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
						Volumes: []corev1.Volume{
							{
								Name: "ca-volume",
								VolumeSource: corev1.VolumeSource{
									Secret: &corev1.SecretVolumeSource{
										SecretName: "ca-secret",
										Items:      []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}},
									},
								},
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "ca-volume",
								MountPath: "/mnt/ca.crt",
								SubPath:   "ca.crt",
							},
						},
					},
					MigrationSource: flywayv1alpha1.MigrationSource{
						Encoding: "UTF-8",
					},
				},
			},
			envAssertions: map[string]string{
				"FLYWAY_USER":     "voluser",
				"FLYWAY_URL":      "jdbc:volurl",
				"FLYWAY_ENCODING": "UTF-8",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := createJobSpec(&tt.migration, 1)
			if job == nil {
				t.Fatalf("createJobSpec returned nil")
			}
			container := job.Spec.Template.Spec.Containers
			if len(container) == 0 {
				t.Fatalf("no containers in job spec")
			}
			env := container[0].Env
			for k, v := range tt.envAssertions {
				found := false
				for _, e := range env {
					if e.Name == k && e.Value == v {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("env var %s=%s not found in job spec", k, v)
				}
			}
			if tt.name == "volume and volumemount" {
				// Assert volume mount
				foundMount := false
				for _, vm := range container[0].VolumeMounts {
					if vm.Name == "ca-volume" && vm.MountPath == "/mnt/ca.crt" && vm.SubPath == "ca.crt" {
						foundMount = true
						break
					}
				}
				if !foundMount {
					t.Errorf("expected volume mount for ca-volume at /mnt/ca.crt with subPath ca.crt not found")
				}
				// Assert volume
				foundVol := false
				for _, vol := range job.Spec.Template.Spec.Volumes {
					if vol.Name == "ca-volume" && vol.Secret != nil && vol.Secret.SecretName == "ca-secret" {
						for _, item := range vol.Secret.Items {
							if item.Key == "ca.crt" && item.Path == "ca.crt" {
								foundVol = true
								break
							}
						}
					}
				}
				if !foundVol {
					t.Errorf("expected secret volume ca-volume with ca.crt not found")
				}
			}
		})
	}
}

func TestCreateJobSpecActiveDeadline(t *testing.T) {
	tests := []struct {
		name     string
		deadline *metav1.Duration
		want     int64
	}{
		{name: "unset uses the default", deadline: nil, want: 3600},
		{name: "whole seconds", deadline: &metav1.Duration{Duration: 30 * time.Minute}, want: 1800},
		{name: "rounded up", deadline: &metav1.Duration{Duration: 1500 * time.Millisecond}, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &flywayv1alpha1.Migration{Spec: flywayv1alpha1.MigrationSpec{ActiveDeadline: tt.deadline}}
			if got := createJobSpec(m, 1).Spec.ActiveDeadlineSeconds; got == nil || *got != tt.want {
				t.Errorf("activeDeadlineSeconds = %d (nil: %t), want %d", ptr.Deref(got, 0), got == nil, tt.want)
			}
		})
	}
}

func TestSetSecurityContext(t *testing.T) {
	restrictedContainer := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
		ReadOnlyRootFilesystem:   ptr.To(true),
	}
	podOverride := &corev1.PodSecurityContext{RunAsUser: ptr.To[int64](0)}
	containerOverride := &corev1.SecurityContext{Privileged: ptr.To(true)}

	tests := []struct {
		name          string
		openShift     bool
		config        flywayv1alpha1.FlywayConfiguration
		wantPod       *corev1.PodSecurityContext
		wantContainer *corev1.SecurityContext
	}{
		{
			name: "Kubernetes runs as UID and fsGroup 1001",
			wantPod: &corev1.PodSecurityContext{
				RunAsNonRoot:        ptr.To(true),
				RunAsUser:           ptr.To(defaultRunAsUser),
				FSGroup:             ptr.To(defaultRunAsUser),
				FSGroupChangePolicy: ptr.To(corev1.FSGroupChangeOnRootMismatch),
				SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			wantContainer: restrictedContainer,
		},
		{
			name:      "OpenShift leaves UID and fsGroup to the SCC",
			openShift: true,
			wantPod: &corev1.PodSecurityContext{
				RunAsNonRoot:        ptr.To(true),
				FSGroupChangePolicy: ptr.To(corev1.FSGroupChangeOnRootMismatch),
				SeccompProfile:      &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
			},
			wantContainer: restrictedContainer,
		},
		{
			name:          "overrides replace the defaults",
			config:        flywayv1alpha1.FlywayConfiguration{PodSecurityContext: podOverride, SecurityContext: containerOverride},
			wantPod:       podOverride,
			wantContainer: containerOverride,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			migration := &flywayv1alpha1.Migration{Spec: flywayv1alpha1.MigrationSpec{FlywayConfiguration: tt.config}}
			podSpec := &createJobSpec(migration, 1).Spec.Template.Spec
			setSecurityContext(podSpec, migration, tt.openShift)

			if !reflect.DeepEqual(podSpec.SecurityContext, tt.wantPod) {
				t.Errorf("pod securityContext = %+v, want %+v", podSpec.SecurityContext, tt.wantPod)
			}
			if podSpec.SecurityContext == tt.config.PodSecurityContext {
				t.Errorf("pod securityContext shares the Migration's pointer")
			}
			if !lo.ContainsBy(podSpec.Volumes, func(v corev1.Volume) bool { return v.Name == tmpVolumeName && v.EmptyDir != nil }) {
				t.Errorf("no %s emptyDir volume in %+v", tmpVolumeName, podSpec.Volumes)
			}
			containers := []*corev1.Container{&podSpec.InitContainers[0], &podSpec.Containers[0]}
			for _, c := range containers {
				if !reflect.DeepEqual(c.SecurityContext, tt.wantContainer) {
					t.Errorf("container %s securityContext = %+v, want %+v", c.Name, c.SecurityContext, tt.wantContainer)
				}
				if c.SecurityContext == tt.config.SecurityContext {
					t.Errorf("container %s securityContext shares the Migration's pointer", c.Name)
				}
				if !lo.ContainsBy(c.VolumeMounts, func(m corev1.VolumeMount) bool { return m.Name == tmpVolumeName && m.MountPath == tmpPath }) {
					t.Errorf("container %s does not mount %s at %s", c.Name, tmpVolumeName, tmpPath)
				}
			}
			if containers[0].SecurityContext == containers[1].SecurityContext {
				t.Errorf("the containers share one securityContext")
			}
		})
	}
}

func TestSetSecurityContextKeepsUserTmp(t *testing.T) {
	userTmp := corev1.VolumeMount{Name: "my-tmp", MountPath: "/tmp/"}
	userVolume := corev1.Volume{Name: tmpVolumeName, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}}
	migration := &flywayv1alpha1.Migration{Spec: flywayv1alpha1.MigrationSpec{
		FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
			VolumeMounts: []corev1.VolumeMount{userTmp},
			Volumes:      []corev1.Volume{userVolume},
		},
	}}
	podSpec := &createJobSpec(migration, 1).Spec.Template.Spec
	setSecurityContext(podSpec, migration, false)

	tmpMounts := lo.Filter(podSpec.Containers[0].VolumeMounts, func(m corev1.VolumeMount, _ int) bool { return path.Clean(m.MountPath) == tmpPath })
	if !reflect.DeepEqual(tmpMounts, []corev1.VolumeMount{userTmp}) {
		t.Errorf("flyway /tmp mounts = %+v, want only the user's %+v", tmpMounts, userTmp)
	}
	tmpVolumes := lo.Filter(podSpec.Volumes, func(v corev1.Volume, _ int) bool { return v.Name == tmpVolumeName })
	if !reflect.DeepEqual(tmpVolumes, []corev1.Volume{userVolume}) {
		t.Errorf("%s volumes = %+v, want only the user's %+v", tmpVolumeName, tmpVolumes, userVolume)
	}
}

// failingMapper fails every lookup with err.
type failingMapper struct {
	meta.RESTMapper
	err error
}

func (m failingMapper) KindFor(schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, m.err
}

func TestIsOpenShift(t *testing.T) {
	openShift := meta.NewDefaultRESTMapper(nil)
	openShift.Add(schema.GroupVersionKind{Group: "security.openshift.io", Version: "v1", Kind: "SecurityContextConstraints"}, meta.RESTScopeRoot)
	staleGroup := apiutil.ErrResourceDiscoveryFailed{
		{Group: "security.openshift.io", Version: "v1"}: &meta.NoResourceMatchError{},
	}

	tests := []struct {
		name    string
		mapper  meta.RESTMapper
		want    bool
		wantErr bool
	}{
		{name: "group served", mapper: openShift, want: true},
		{name: "group absent", mapper: meta.NewDefaultRESTMapper(nil), want: false},
		{name: "group listed but its discovery failed", mapper: failingMapper{err: &staleGroup}, wantErr: true},
		{name: "other lookup error", mapper: failingMapper{err: errors.New("connection refused")}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IsOpenShift(tt.mapper)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("IsOpenShift = %v, want %v", got, tt.want)
			}
		})
	}
}
