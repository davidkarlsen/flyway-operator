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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

const (
	samplePath    = "../../config/samples/flyway_v1alpha1_migration.yaml"
	usageDocPath  = "../../docs/usage.md"
	docsGlobUsage = "../../docs/*.md"
	docsGlobExtra = "../../docs/examples/*.md"
)

var yamlBlock = regexp.MustCompile("(?s)```yaml\n(.*?)```")

// documentedMigrations returns every YAML document of kind Migration found in fenced
// ```yaml blocks of the given markdown files, keyed by "file#index".
func documentedMigrations(files ...string) map[string]*unstructured.Unstructured {
	out := map[string]*unstructured.Unstructured{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		Expect(err).NotTo(HaveOccurred())
		for bi, m := range yamlBlock.FindAllStringSubmatch(string(raw), -1) {
			for di, doc := range strings.Split(m[1], "\n---") {
				u := &unstructured.Unstructured{}
				if err := yaml.Unmarshal([]byte(doc), &u.Object); err != nil || u.GetKind() != "Migration" {
					continue
				}
				out[fmt.Sprintf("%s#%d.%d", filepath.Base(f), bi, di)] = u
			}
		}
	}
	return out
}

func loadSample() *unstructured.Unstructured {
	raw, err := os.ReadFile(samplePath)
	Expect(err).NotTo(HaveOccurred())
	u := &unstructured.Unstructured{}
	Expect(yaml.Unmarshal(raw, &u.Object)).To(Succeed())
	return u
}

// expectSubset fails if any leaf value in sub differs from the value at the same path in full.
func expectSubset(sub, full map[string]interface{}, path string) {
	for k, sv := range sub {
		p := path + "." + k
		fv, ok := full[k]
		Expect(ok).To(BeTrue(), "%s is in the OLM sample but not in the docs/usage.md example", p)
		sm, sIsMap := sv.(map[string]interface{})
		fm, fIsMap := fv.(map[string]interface{})
		if sIsMap && fIsMap {
			expectSubset(sm, fm, p)
			continue
		}
		Expect(sv).To(Equal(fv), "%s differs between the OLM sample and the docs/usage.md example", p)
	}
}

var _ = Describe("Documented examples", func() {
	var ns string

	BeforeEach(func() {
		n := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "docs-"}}
		Expect(k8sClient.Create(context.Background(), n)).To(Succeed())
		ns = n.Name
		DeferCleanup(func() { _ = k8sClient.Delete(context.Background(), n) })
	})

	createDryRun := func(u *unstructured.Unstructured) error {
		u = u.DeepCopy()
		u.SetNamespace(ns)
		return k8sClient.Create(context.Background(), u, client.DryRunAll)
	}

	It("validates every Migration in docs/ against the CRD", func() {
		files, err := filepath.Glob(docsGlobUsage)
		Expect(err).NotTo(HaveOccurred())
		extra, err := filepath.Glob(docsGlobExtra)
		Expect(err).NotTo(HaveOccurred())
		docs := documentedMigrations(append(files, extra...)...)
		Expect(docs).NotTo(BeEmpty())
		for key, u := range docs {
			Expect(createDryRun(u)).To(Succeed(), "documented example %s is rejected by the CRD", key)
		}
	})

	It("validates the OLM sample against the CRD", func() {
		Expect(createDryRun(loadSample())).To(Succeed())
	})

	It("keeps the OLM sample a subset of the docs/usage.md example", func() {
		docs := documentedMigrations(usageDocPath)
		Expect(docs).To(HaveLen(1), "docs/usage.md should have exactly one reference Migration example")
		var reference *unstructured.Unstructured
		for _, u := range docs {
			reference = u
		}
		sample := loadSample()
		Expect(sample.GetAPIVersion()).To(Equal(reference.GetAPIVersion()))
		Expect(sample.GetName()).To(Equal(reference.GetName()))
		expectSubset(sample.Object["spec"].(map[string]interface{}), reference.Object["spec"].(map[string]interface{}), "spec")
	})
})
