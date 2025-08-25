package controller

import (
	"strconv"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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
