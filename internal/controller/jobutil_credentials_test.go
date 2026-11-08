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

package controller

import (
	"slices"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func envNames(envs []corev1.EnvVar) []string {
	names := make([]string, 0, len(envs))
	for _, e := range envs {
		names = append(names, e.Name)
	}
	return names
}

func TestCreateJobSpecWithoutCredentialsHasNoPassword(t *testing.T) {
	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
		Spec: flywayv1alpha1.MigrationSpec{
			Database: flywayv1alpha1.Database{
				Username: "app",
				JdbcUrl:  "jdbc:sqlserver://h:1433;authentication=ActiveDirectoryDefault",
			},
		},
	}

	names := envNames(createJobSpec(migration, 1).Spec.Template.Spec.Containers[0].Env)

	if slices.Contains(names, "FLYWAY_PASSWORD") {
		t.Errorf("FLYWAY_PASSWORD set without credentials: %v", names)
	}
	if !slices.Contains(names, "FLYWAY_USER") || !slices.Contains(names, "FLYWAY_URL") {
		t.Errorf("FLYWAY_USER and FLYWAY_URL expected: %v", names)
	}
}

func TestCreateJobSpecWithoutUsernameHasNoUser(t *testing.T) {
	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
		Spec:       flywayv1alpha1.MigrationSpec{Database: flywayv1alpha1.Database{JdbcUrl: "jdbc:testurl"}},
	}

	if names := envNames(createJobSpec(migration, 1).Spec.Template.Spec.Containers[0].Env); slices.Contains(names, "FLYWAY_USER") {
		t.Errorf("FLYWAY_USER set without username: %v", names)
	}
}

func TestCreateJobSpecCredentialsBecomePasswordAndDoNotAlias(t *testing.T) {
	migration := &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
		Spec: flywayv1alpha1.MigrationSpec{Database: flywayv1alpha1.Database{
			Username: "app", JdbcUrl: "jdbc:testurl",
			Credentials: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "db"}, Key: "password"},
		}},
	}
	want := migration.Spec.DeepCopy()

	env := createJobSpec(migration, 1).Spec.Template.Spec.Containers[0].Env
	i := slices.IndexFunc(env, func(e corev1.EnvVar) bool { return e.Name == "FLYWAY_PASSWORD" })
	if i < 0 || env[i].ValueFrom == nil || env[i].ValueFrom.SecretKeyRef.Name != "db" || env[i].ValueFrom.SecretKeyRef.Key != "password" {
		t.Fatalf("FLYWAY_PASSWORD = %+v, want secret db/password", env)
	}
	env[i].ValueFrom.SecretKeyRef.Name = "mutated"

	if !equality.Semantic.DeepEqual(&migration.Spec, want) {
		t.Errorf("mutating the Job changed the Migration spec: got %+v", migration.Spec)
	}
}
