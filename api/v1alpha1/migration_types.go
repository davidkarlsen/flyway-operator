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
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MigrationStatus defines the observed state of Migration
type MigrationStatus struct {
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

func (m *Migration) GetConditions() []metav1.Condition {
	return m.Status.Conditions
}

func (m *Migration) SetConditions(conditions []metav1.Condition) {
	m.Status.Conditions = conditions
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Migration is the Schema for the migrations API
type Migration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MigrationSpec   `json:"spec,omitempty"`
	Status MigrationStatus `json:"status,omitempty"`
}

// MigrationSpec defines the desired state of Migration
type MigrationSpec struct {
	// settings for database connection
	// +kubebuilder:validation:Required
	Database Database `json:"database"`

	// settings defining the SQL migrations
	// +kubebuilder:validation:Required
	Migration MigrationSource `json:"migrationSource"`
}

// Database defines the database-settings
type Database struct {
	// username for connecting to database
	// +kubebuilder:validation:Required
	Username string `json:"username"`
	// reference to a secret containing the password for connecting to database
	// +kubebuilder:validation:Required
	Credentials v1.SecretKeySelector `json:"credentials"`
	// the jdbcUrl to connect to database
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`^jdbc:.*`
	JdbcUrl string `json:"jdbcUrl"`
}

func (r *Migration) GetCredentials() v1.SecretReference {
	return v1.SecretReference{
		Name:      r.Spec.Database.Credentials.Name,
		Namespace: r.Namespace,
	}
}

// MigrationSource defines the source for the flyway-migrations.
type MigrationSource struct {
	// Reference to the image holding the SQLs to migrate
	// +kubebuilder:validation:Required
	ImageRef string `json:"imageRef"`

	// Path within the image to the SQLs for flyway
	// +kubebuilder:default="/sql"
	SqlPath string `json:"path"`
}

//+kubebuilder:object:root=true

// MigrationList contains a list of Migration
type MigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Migration `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Migration{}, &MigrationList{})
}
