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

	v1 "k8s.io/api/core/v1"
)

func TestDatabaseDeepCopyDoesNotShareCredentials(t *testing.T) {
	orig := Database{Credentials: &v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: "db"}, Key: "password"}}

	cp := orig.DeepCopy()
	cp.Credentials.Name = "mutated"
	cp.Credentials.Key = "mutated"

	if orig.Credentials.Name != "db" || orig.Credentials.Key != "password" {
		t.Errorf("mutating the copy changed the original: %+v", *orig.Credentials)
	}
}

func TestGetCredentialsWithoutCredentials(t *testing.T) {
	m := &Migration{}
	m.Namespace = "ns"

	if got := m.GetCredentials(); got != (v1.SecretReference{}) {
		t.Errorf("GetCredentials() without credentials = %+v, want the zero reference", got)
	}
}

func TestGetCredentialsWithCredentials(t *testing.T) {
	m := &Migration{Spec: MigrationSpec{Database: Database{Credentials: &v1.SecretKeySelector{LocalObjectReference: v1.LocalObjectReference{Name: "db"}}}}}
	m.Namespace = "ns"

	if got := m.GetCredentials(); got != (v1.SecretReference{Name: "db", Namespace: "ns"}) {
		t.Errorf("GetCredentials() = %+v, want db in ns", got)
	}
}
