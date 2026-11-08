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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var (
	yamlBlock = regexp.MustCompile("(?s)```ya?ml\\r?\\n(.*?)```")
	docSep    = regexp.MustCompile(`(?m)^---\s*$`)
)

// documentedMigrations returns every YAML document of kind Migration found in fenced
// ```yaml/```yml blocks of the given markdown files, keyed by "file#index".
func documentedMigrations(files ...string) map[string]*unstructured.Unstructured {
	out := map[string]*unstructured.Unstructured{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		Expect(err).NotTo(HaveOccurred())
		for bi, m := range yamlBlock.FindAllStringSubmatch(string(raw), -1) {
			for di, doc := range docSep.Split(m[1], -1) {
				u := &unstructured.Unstructured{}
				err := yaml.Unmarshal([]byte(doc), &u.Object)
				if err != nil {
					Expect(doc).NotTo(MatchRegexp(`(?m)^kind:\s*Migration\b`), "%s block %d doc %d contains kind: Migration but does not parse: %v", f, bi, di, err)
					continue
				}
				if u.GetKind() != "Migration" {
					continue
				}
				key := fmt.Sprintf("%s#%d.%d", f, bi, di)
				Expect(out).NotTo(HaveKey(key), "duplicate documented example key %s", key)
				out[key] = u
			}
		}
	}
	return out
}

// referenceMigration returns the single Migration named name, failing when there are none or several.
func referenceMigration(docs map[string]*unstructured.Unstructured, name string) (*unstructured.Unstructured, error) {
	var matches []*unstructured.Unstructured
	for _, u := range docs {
		if u.GetName() == name {
			matches = append(matches, u)
		}
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("docs/usage.md needs exactly one Migration named %q as the OLM sample's reference, found %d", name, len(matches))
	}
	return matches[0], nil
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
	createDryRun := func(u *unstructured.Unstructured) error {
		u = u.DeepCopy()
		u.SetNamespace("default")
		return k8sClient.Create(context.Background(), u, client.DryRunAll, client.FieldValidation("Strict"))
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

	It("rejects unknown fields", func() {
		u := loadSample()
		Expect(unstructured.SetNestedField(u.Object, "x", "spec", "bogusField")).To(Succeed())
		Expect(createDryRun(u)).NotTo(Succeed())
	})

	It("accepts additional differently named Migrations next to the reference", func() {
		sample := loadSample()
		tmp := filepath.Join(GinkgoT().TempDir(), "usage.md")
		md := "```yaml\napiVersion: flyway.davidkarlsen.com/v1alpha1\nkind: Migration\nmetadata:\n  name: " + sample.GetName() + "\n```\n" +
			"```yaml\napiVersion: flyway.davidkarlsen.com/v1alpha1\nkind: Migration\nmetadata:\n  name: other-use-case\n```\n"
		Expect(os.WriteFile(tmp, []byte(md), 0o600)).To(Succeed())
		docs := documentedMigrations(tmp)
		Expect(docs).To(HaveLen(2))
		ref, err := referenceMigration(docs, sample.GetName())
		Expect(err).NotTo(HaveOccurred())
		Expect(ref.GetName()).To(Equal(sample.GetName()))
		_, err = referenceMigration(docs, "missing")
		Expect(err).To(MatchError(ContainSubstring("exactly one Migration named")))
		docs["dup"] = ref
		_, err = referenceMigration(docs, sample.GetName())
		Expect(err).To(HaveOccurred())
	})

	It("keeps the OLM sample a subset of the docs/usage.md example", func() {
		docs := documentedMigrations(usageDocPath)
		sample := loadSample()
		reference, err := referenceMigration(docs, sample.GetName())
		Expect(err).NotTo(HaveOccurred())
		Expect(sample.GetAPIVersion()).To(Equal(reference.GetAPIVersion()))
		sampleSpec, found, err := unstructured.NestedMap(sample.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue(), "OLM sample has no spec")
		refSpec, found, err := unstructured.NestedMap(reference.Object, "spec")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue(), "docs/usage.md example has no spec")
		expectSubset(sampleSpec, refSpec, "spec")
	})
})
