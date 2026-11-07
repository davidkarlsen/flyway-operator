package controller

import (
	"slices"
	"strconv"
	"strings"
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
						ImageRef: "registry.example.com/sql:1",
						SqlPath:  "/sql",
						Encoding: "UTF-8",
					},
				},
			},
			envAssertions: map[string]string{
				"FLYWAY_USER":                      "testuser",
				"FLYWAY_URL":                       "jdbc:testurl",
				"FLYWAY_ENCODING":                  "UTF-8",
				"FLYWAY_LOCATIONS":                 "filesystem:/flyway/migration-source/sql",
				"FLYWAY_FAIL_ON_MISSING_LOCATIONS": "true",
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
			if n := len(job.Spec.Template.Spec.InitContainers); n != 0 {
				t.Errorf("expected no init containers, got %d", n)
			}
			sqlVol := job.Spec.Template.Spec.Volumes[0]
			if sqlVol.Name != sqlVolumeName || sqlVol.Image == nil {
				t.Fatalf("expected image volume %q first, got %+v", sqlVolumeName, sqlVol)
			}
			if sqlVol.Image.Reference != tt.migration.Spec.MigrationSource.ImageRef {
				t.Errorf("image volume reference = %q, want %q", sqlVol.Image.Reference, tt.migration.Spec.MigrationSource.ImageRef)
			}
			if sqlVol.Image.PullPolicy != corev1.PullAlways {
				t.Errorf("image volume pullPolicy = %q, want Always", sqlVol.Image.PullPolicy)
			}
			sqlMount := container[0].VolumeMounts[0]
			if sqlMount.Name != sqlVolumeName || sqlMount.MountPath != sqlMountPath || !sqlMount.ReadOnly {
				t.Errorf("expected read-only mount of %q at %s, got %+v", sqlVolumeName, sqlMountPath, sqlMount)
			}
			if slices.ContainsFunc(container[0].Args, func(a string) bool { return strings.HasPrefix(a, "-locations") }) {
				t.Errorf("args %v must not force -locations", container[0].Args)
			}
			if !hasSourceImageVolume(job, &tt.migration) {
				t.Errorf("hasSourceImageVolume = false for the created job")
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

func TestSqlLocation(t *testing.T) {
	for _, tt := range []struct{ path, want string }{
		{"/sql", "filesystem:/flyway/migration-source/sql"},
		{"sql", "filesystem:/flyway/migration-source/sql"},
		{"/sql/", "filesystem:/flyway/migration-source/sql"},
		{"", "filesystem:/flyway/migration-source"},
		{"/", "filesystem:/flyway/migration-source"},
		{"/a/b/", "filesystem:/flyway/migration-source/a/b"},
	} {
		m := &flywayv1alpha1.Migration{Spec: flywayv1alpha1.MigrationSpec{MigrationSource: flywayv1alpha1.MigrationSource{SqlPath: tt.path}}}
		if got := sqlLocation(m); got != tt.want {
			t.Errorf("sqlLocation(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// User envVars come after the operator's, so a user FLYWAY_LOCATIONS wins (Kubernetes uses the last duplicate).
func TestUserLocationsOverride(t *testing.T) {
	m := &flywayv1alpha1.Migration{Spec: flywayv1alpha1.MigrationSpec{
		FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
			EnvVars: []corev1.EnvVar{{Name: "FLYWAY_LOCATIONS", Value: "filesystem:/flyway/migration-source/sql,filesystem:/extra"}},
		},
		MigrationSource: flywayv1alpha1.MigrationSource{ImageRef: "r/i:1", SqlPath: "/sql"},
	}}
	env := createJobSpec(m, 1).Spec.Template.Spec.Containers[0].Env
	last := ""
	for _, e := range env {
		if e.Name == "FLYWAY_LOCATIONS" {
			last = e.Value
		}
	}
	if last != "filesystem:/flyway/migration-source/sql,filesystem:/extra" {
		t.Errorf("last FLYWAY_LOCATIONS = %q, want the user value", last)
	}
}

func TestGetFlywayArgsDoesNotAliasCommands(t *testing.T) {
	commands := make([]string, 1, 4)
	commands[0] = "migrate"
	m := &flywayv1alpha1.Migration{Spec: flywayv1alpha1.MigrationSpec{FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{Commands: commands}}}
	_ = getFlywayArgs(m)
	if got := commands[:cap(commands)][1]; got != "" {
		t.Errorf("getFlywayArgs wrote %q into the spec's Commands backing array", got)
	}
}
