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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMigration_IsPaused(t *testing.T) {
	tests := []struct {
		name        string
		annotations map[string]string
		want        bool
	}{
		{
			name: "migration is paused",
			annotations: map[string]string{
				Prefix + "/paused": "true",
			},
			want: true,
		},
		{
			name: "migration is not paused",
			annotations: map[string]string{
				Prefix + "/paused": "false",
			},
			want: false,
		},
		{
			name:        "no pause annotation",
			annotations: map[string]string{},
			want:        false,
		},
		{
			name: "other annotations only",
			annotations: map[string]string{
				"some-other-annotation": "value",
			},
			want: false,
		},
		{
			name:        "nil annotations",
			annotations: nil,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Migration{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: tt.annotations,
				},
			}
			got := m.IsPaused()
			if got != tt.want {
				t.Errorf("IsPaused() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMigration_GenerationAsString(t *testing.T) {
	tests := []struct {
		name       string
		generation int64
		want       string
	}{
		{
			name:       "generation 0",
			generation: 0,
			want:       "0",
		},
		{
			name:       "generation 1",
			generation: 1,
			want:       "1",
		},
		{
			name:       "generation 42",
			generation: 42,
			want:       "42",
		},
		{
			name:       "large generation",
			generation: 9999,
			want:       "9999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Migration{
				ObjectMeta: metav1.ObjectMeta{
					Generation: tt.generation,
				},
			}
			got := m.GenerationAsString()
			if got != tt.want {
				t.Errorf("GenerationAsString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMigration_GetConditions(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
		},
		{
			Type:   "Failed",
			Status: metav1.ConditionFalse,
		},
	}

	m := &Migration{
		Status: MigrationStatus{
			Conditions: conditions,
		},
	}

	got := m.GetConditions()
	if len(got) != len(conditions) {
		t.Errorf("GetConditions() length = %v, want %v", len(got), len(conditions))
	}

	for i, cond := range got {
		if cond.Type != conditions[i].Type || cond.Status != conditions[i].Status {
			t.Errorf("GetConditions()[%d] = %v, want %v", i, cond, conditions[i])
		}
	}
}

func TestMigration_SetConditions(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
		},
	}

	m := &Migration{}
	m.SetConditions(conditions)

	got := m.Status.Conditions
	if len(got) != len(conditions) {
		t.Errorf("SetConditions() length = %v, want %v", len(got), len(conditions))
	}

	if got[0].Type != conditions[0].Type || got[0].Status != conditions[0].Status {
		t.Errorf("SetConditions() = %v, want %v", got[0], conditions[0])
	}
}

func TestMigration_GetCredentials(t *testing.T) {
	m := &Migration{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "test-namespace",
		},
		Spec: MigrationSpec{
			Database: Database{
				Credentials: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "test-secret",
					},
					Key: "password",
				},
			},
		},
	}

	got := m.GetCredentials()
	if got.Name != "test-secret" {
		t.Errorf("GetCredentials().Name = %v, want test-secret", got.Name)
	}
	if got.Namespace != "test-namespace" {
		t.Errorf("GetCredentials().Namespace = %v, want test-namespace", got.Namespace)
	}
}

func TestMigrationSource_GetPlaceholdersAsEnvVars(t *testing.T) {
	tests := []struct {
		name         string
		placeholders map[string]string
		wantCount    int
	}{
		{
			name:         "no placeholders",
			placeholders: nil,
			wantCount:    0,
		},
		{
			name:         "empty placeholders",
			placeholders: map[string]string{},
			wantCount:    0,
		},
		{
			name: "single placeholder",
			placeholders: map[string]string{
				"DATABASE": "mydb",
			},
			wantCount: 1,
		},
		{
			name: "multiple placeholders",
			placeholders: map[string]string{
				"DATABASE": "mydb",
				"SCHEMA":   "public",
				"TABLE":    "users",
			},
			wantCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms := &MigrationSource{
				Placeholders: tt.placeholders,
			}

			got := ms.GetPlaceholdersAsEnvVars()
			if len(got) != tt.wantCount {
				t.Errorf("GetPlaceholdersAsEnvVars() length = %v, want %v", len(got), tt.wantCount)
			}

			// Verify format of env vars
			for _, envVar := range got {
				if len(envVar.Name) < len("FLYWAY_PLACEHOLDERS_") {
					t.Errorf("GetPlaceholdersAsEnvVars() env var name too short: %v", envVar.Name)
				}
				if envVar.Name[:len("FLYWAY_PLACEHOLDERS_")] != "FLYWAY_PLACEHOLDERS_" {
					t.Errorf("GetPlaceholdersAsEnvVars() env var name doesn't start with FLYWAY_PLACEHOLDERS_: %v", envVar.Name)
				}
			}

			// Verify all placeholders are included
			envVarMap := make(map[string]string)
			for _, envVar := range got {
				envVarMap[envVar.Name] = envVar.Value
			}

			for key, value := range tt.placeholders {
				expectedName := "FLYWAY_PLACEHOLDERS_" + key
				if envVarMap[expectedName] != value {
					t.Errorf("GetPlaceholdersAsEnvVars() missing or incorrect placeholder: %v=%v", expectedName, value)
				}
			}
		})
	}
}
