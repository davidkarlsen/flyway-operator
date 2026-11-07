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
	"maps"
	"testing"
)

func TestObjectMetadataGetters(t *testing.T) {
	var unset *ObjectMetadata
	if got := unset.GetLabels(); got != nil {
		t.Errorf("nil ObjectMetadata: GetLabels() = %v, want nil", got)
	}
	if got := unset.GetAnnotations(); got != nil {
		t.Errorf("nil ObjectMetadata: GetAnnotations() = %v, want nil", got)
	}

	empty := &ObjectMetadata{}
	if got := empty.GetLabels(); got != nil {
		t.Errorf("empty ObjectMetadata: GetLabels() = %v, want nil", got)
	}
	if got := empty.GetAnnotations(); got != nil {
		t.Errorf("empty ObjectMetadata: GetAnnotations() = %v, want nil", got)
	}

	o := &ObjectMetadata{
		Labels:      map[string]LabelValue{"team": "payments", "empty": ""},
		Annotations: map[string]string{"sidecar.istio.io/inject": "false"},
	}
	wantLabels := map[string]string{"team": "payments", "empty": ""}
	labels := o.GetLabels()
	if !maps.Equal(labels, wantLabels) {
		t.Errorf("GetLabels() = %v, want %v", labels, wantLabels)
	}
	labels["team"] = "mutated"
	if o.Labels["team"] != "payments" {
		t.Errorf("mutating GetLabels() changed the ObjectMetadata: %v", o.Labels)
	}
	if got := o.GetAnnotations(); !maps.Equal(got, o.Annotations) {
		t.Errorf("GetAnnotations() = %v, want %v", got, o.Annotations)
	}
}
