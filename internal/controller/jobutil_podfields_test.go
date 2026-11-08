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
	"maps"
	"testing"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podFieldsMigration() *flywayv1alpha1.Migration {
	return &flywayv1alpha1.Migration{
		ObjectMeta: metav1.ObjectMeta{Name: "my-app"},
		Spec: flywayv1alpha1.MigrationSpec{
			FlywayConfiguration: flywayv1alpha1.FlywayConfiguration{
				Resources: &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
					Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
				},
				NodeSelector: map[string]string{"disktype": "ssd"},
				Tolerations:  []corev1.Toleration{{Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "db", Effect: corev1.TaintEffectNoSchedule}},
				Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}}}},
					}},
				}},
				PriorityClassName:  "batch-low",
				ServiceAccountName: "flyway",
			},
		},
	}
}

func TestCreateJobSpecPodFields(t *testing.T) {
	migration := podFieldsMigration()
	config := migration.Spec.FlywayConfiguration

	pod := createJobSpec(migration, 1).Spec.Template.Spec

	if len(pod.Containers) != 1 {
		t.Fatalf("containers = %d, want 1", len(pod.Containers))
	}
	if !equality.Semantic.DeepEqual(pod.Containers[0].Resources, *config.Resources) {
		t.Errorf("resources = %+v, want %+v", pod.Containers[0].Resources, *config.Resources)
	}
	if !maps.Equal(pod.NodeSelector, config.NodeSelector) {
		t.Errorf("nodeSelector = %v", pod.NodeSelector)
	}
	if !equality.Semantic.DeepEqual(pod.Tolerations, config.Tolerations) {
		t.Errorf("tolerations = %v", pod.Tolerations)
	}
	if !equality.Semantic.DeepEqual(pod.Affinity, config.Affinity) {
		t.Errorf("affinity = %v", pod.Affinity)
	}
	if pod.PriorityClassName != "batch-low" {
		t.Errorf("priorityClassName = %q", pod.PriorityClassName)
	}
	if pod.ServiceAccountName != "flyway" {
		t.Errorf("serviceAccountName = %q", pod.ServiceAccountName)
	}
}

func TestCreateJobSpecPodFieldsUnsetKeepsDefaults(t *testing.T) {
	pod := createJobSpec(&flywayv1alpha1.Migration{ObjectMeta: metav1.ObjectMeta{Name: "my-app"}}, 1).Spec.Template.Spec

	if r := pod.Containers[0].Resources; len(r.Requests) != 0 || len(r.Limits) != 0 || len(r.Claims) != 0 {
		t.Errorf("resources = %+v, want none", r)
	}
	if pod.NodeSelector != nil || pod.Tolerations != nil || pod.Affinity != nil || pod.PriorityClassName != "" || pod.ServiceAccountName != "" {
		t.Errorf("pod scheduling fields set without spec: %+v", pod)
	}
}

func TestCreateJobSpecDoesNotAliasPodFields(t *testing.T) {
	migration := podFieldsMigration()
	want := migration.Spec.DeepCopy()

	pod := &createJobSpec(migration, 1).Spec.Template.Spec
	pod.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("9")
	pod.Containers[0].Resources.Limits[corev1.ResourceCPU] = resource.MustParse("9")
	pod.NodeSelector["disktype"] = "mutated"
	pod.NodeSelector["added"] = "x"
	pod.Tolerations[0].Key = "mutated"
	pod.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Values[0] = "mutated"

	if !equality.Semantic.DeepEqual(&migration.Spec, want) {
		t.Errorf("mutating the Job changed the Migration spec: got %+v, want %+v", migration.Spec, *want)
	}
}
