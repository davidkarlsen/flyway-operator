package controller

import (
	"maps"
	"strconv"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
