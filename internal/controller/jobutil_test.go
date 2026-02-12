package controller

import (
	"os"
	"strconv"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
			job := createJobSpec(&tt.migration)
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

func TestJobIsCurrent(t *testing.T) {
	tests := []struct {
		name       string
		job        *batchv1.Job
		migration  *flywayv1alpha1.Migration
		wantResult bool
	}{
		{
			name: "job is current",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						flywayv1alpha1.Generation: "5",
					},
				},
			},
			migration: &flywayv1alpha1.Migration{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 5,
				},
			},
			wantResult: true,
		},
		{
			name: "job is not current - different generation",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						flywayv1alpha1.Generation: "3",
					},
				},
			},
			migration: &flywayv1alpha1.Migration{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 5,
				},
			},
			wantResult: false,
		},
		{
			name: "job has no generation annotation",
			job: &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			migration: &flywayv1alpha1.Migration{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 5,
				},
			},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := jobIsCurrent(tt.job, tt.migration)
			if result != tt.wantResult {
				t.Errorf("jobIsCurrent() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

func TestIsJobFinished(t *testing.T) {
	tests := []struct {
		name       string
		job        *batchv1.Job
		wantResult bool
	}{
		{
			name: "job is complete",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobComplete,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			wantResult: true,
		},
		{
			name: "job is failed",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			wantResult: true,
		},
		{
			name: "job is running",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobComplete,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			wantResult: false,
		},
		{
			name: "job has no conditions",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Conditions: []batchv1.JobCondition{},
				},
			},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isJobFinished(tt.job)
			if result != tt.wantResult {
				t.Errorf("isJobFinished() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

func TestHasFailed(t *testing.T) {
	tests := []struct {
		name       string
		job        *batchv1.Job
		wantResult bool
	}{
		{
			name: "job has failed",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 1,
				},
			},
			wantResult: true,
		},
		{
			name: "job has failed multiple times",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 3,
				},
			},
			wantResult: true,
		},
		{
			name: "job has not failed",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 0,
				},
			},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasFailed(tt.job)
			if result != tt.wantResult {
				t.Errorf("hasFailed() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

func TestHasSucceeded(t *testing.T) {
	tests := []struct {
		name       string
		job        *batchv1.Job
		wantResult bool
	}{
		{
			name: "job has succeeded",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Succeeded: 1,
				},
			},
			wantResult: true,
		},
		{
			name: "job has not succeeded",
			job: &batchv1.Job{
				Status: batchv1.JobStatus{
					Succeeded: 0,
				},
			},
			wantResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasSucceeded(tt.job)
			if result != tt.wantResult {
				t.Errorf("hasSucceeded() = %v, want %v", result, tt.wantResult)
			}
		})
	}
}

func TestGetFlywayImage(t *testing.T) {
	tests := []struct {
		name      string
		migration *flywayv1alpha1.Migration
		envValue  string
		want      string
	}{
		{
			name: "custom image in spec",
			migration: &flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
						FlywayImage: "custom/flyway:latest",
					},
				},
			},
			want: "custom/flyway:latest",
		},
		{
			name: "default image when spec is empty",
			migration: &flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{},
				},
			},
			want: defaultFlywayImage,
		},
		{
			name: "image from environment variable",
			migration: &flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{},
				},
			},
			envValue: "env/flyway:v9",
			want:     "env/flyway:v9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv(envNameFlywayImage, tt.envValue)
				defer os.Unsetenv(envNameFlywayImage)
			} else {
				os.Unsetenv(envNameFlywayImage)
			}
			got := getFlywayImage(tt.migration)
			if got != tt.want {
				t.Errorf("getFlywayImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetFlywayArgs(t *testing.T) {
	tests := []struct {
		name      string
		migration *flywayv1alpha1.Migration
		want      []string
	}{
		{
			name: "basic commands only",
			migration: &flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
						Commands: []string{"info", "migrate"},
					},
				},
			},
			want: []string{"info", "migrate", "-outputType=json"},
		},
		{
			name: "commands with jdbc properties",
			migration: &flywayv1alpha1.Migration{
				Spec: flywayv1alpha1.MigrationSpec{
					FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
						Commands: []string{"migrate"},
						JdbcProperties: map[string]string{
							"ssl":             "true",
							"sslmode":         "require",
							"connectTimeout":  "30",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getFlywayArgs(tt.migration)
			
			if tt.name == "basic commands only" {
				if len(got) != len(tt.want) {
					t.Errorf("getFlywayArgs() length = %v, want %v", len(got), len(tt.want))
				}
				for i, arg := range tt.want {
					if got[i] != arg {
						t.Errorf("getFlywayArgs()[%d] = %v, want %v", i, got[i], arg)
					}
				}
			} else if tt.name == "commands with jdbc properties" {
				// Check that we have the command and output type
				if got[0] != "migrate" {
					t.Errorf("getFlywayArgs()[0] = %v, want migrate", got[0])
				}
				if got[1] != "-outputType=json" {
					t.Errorf("getFlywayArgs()[1] = %v, want -outputType=json", got[1])
				}
				// Check that jdbc properties are included
				if len(got) < 5 {
					t.Errorf("getFlywayArgs() length = %v, expected at least 5 (command + outputType + 3 jdbc properties)", len(got))
				}
				// Verify at least one jdbc property is formatted correctly
				foundJdbcProp := false
				for _, arg := range got {
					if len(arg) > len("-environments.default.jdbcProperties.") {
						if arg[:len("-environments.default.jdbcProperties.")] == "-environments.default.jdbcProperties." {
							foundJdbcProp = true
							break
						}
					}
				}
				if !foundJdbcProp {
					t.Errorf("getFlywayArgs() did not contain expected jdbc property format")
				}
			}
		})
	}
}
