/*
Copyright 2026.

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
	"os"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const kustomizeRulesPath = "../../config/prometheus/rules.yaml"

// The shipped PrometheusRule has the operator and Migration groups and is part of the prometheus kustomization.
func TestShippedPrometheusRule(t *testing.T) {
	raw, err := os.ReadFile(kustomizeRulesPath)
	if err != nil {
		t.Fatal(err)
	}
	var rule struct {
		Kind string `json:"kind"`
		Spec struct {
			Groups []struct {
				Name  string `json:"name"`
				Rules []struct {
					Alert string `json:"alert"`
				} `json:"rules"`
			} `json:"groups"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &rule); err != nil {
		t.Fatal(err)
	}
	if rule.Kind != "PrometheusRule" {
		t.Fatalf("kind = %q, want PrometheusRule", rule.Kind)
	}

	got := map[string]int{}
	for _, g := range rule.Spec.Groups {
		got[g.Name] = len(g.Rules)
		for _, r := range g.Rules {
			if r.Alert == "" {
				t.Errorf("group %s has a rule without an alert name", g.Name)
			}
		}
	}
	want := map[string]int{"flyway-operator.operator": 2, "flyway-operator.migrations": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rule groups = %v, want %v", got, want)
	}

	kustomization, err := os.ReadFile("../../config/prometheus/kustomization.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kustomization), "- rules.yaml") {
		t.Fatal("config/prometheus/kustomization.yaml does not include rules.yaml")
	}
}
