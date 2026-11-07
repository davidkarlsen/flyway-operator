//go:build e2e

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

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	"github.com/davidkarlsen/flyway-operator/test/utils"
)

const (
	// jobTimeout covers image pulls, the Flyway JVM start and, for failures, the Job's three pod attempts.
	jobTimeout   = 6 * time.Minute
	pollInterval = 5 * time.Second
)

var _ = Describe("Migration", func() {
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			dumpDiagnostics(CurrentSpecReport().LeafNodeText)
		}
	})

	// The upgrade builds on the first apply, so only these two are ordered.
	Describe("lifecycle", Ordered, func() {
		It("applies the migrations to the database with the operator's default Flyway image", func() {
			applyMigration("happy", "v1", "happy", "postgres", "", nil)

			m := waitForCondition("happy", flywayv1alpha1.ConditionApplied, flywayv1alpha1.ReasonJobSucceeded, 0)
			Expect(m.Status.ObservedGeneration).To(Equal(m.Generation))
			Expect(mustRun(exec.Command("kubectl", "-n", namespace, "get", "job", "happy",
				"-o", "jsonpath={.spec.template.spec.containers[0].image}"))).To(Equal(flywayImage))

			Expect(psql("SELECT version, success FROM happy.flyway_schema_history WHERE type = 'SQL' ORDER BY installed_rank")).
				To(Equal("1|t"))
			Expect(psql("SELECT to_regclass('happy.person')")).To(Equal("happy.person"))
		})

		It("applies a new migration when the source image changes", func() {
			previous := getMigration("happy").Generation
			applyMigration("happy", "v2", "happy", "postgres", "", nil)

			m := waitForCondition("happy", flywayv1alpha1.ConditionApplied, flywayv1alpha1.ReasonJobSucceeded, previous+1)
			Expect(m.Status.ObservedGeneration).To(Equal(m.Generation))

			Expect(psql("SELECT version, success FROM happy.flyway_schema_history WHERE type = 'SQL' ORDER BY installed_rank")).
				To(Equal("1|t\n2|t"))
			Expect(psql("SELECT count(*) FROM information_schema.columns " +
				"WHERE table_schema = 'happy' AND table_name = 'person' AND column_name = 'email'")).To(Equal("1"))
		})
	})

	It("reports Failed when the SQL is broken", func() {
		retryLimit := 0
		// Sets spec.flywayConfiguration.flywayImage, covering the override path.
		applyMigration("broken", "broken", "broken", "postgres", flywayImage, &retryLimit)

		m := waitForCondition("broken", flywayv1alpha1.ConditionFailed, flywayv1alpha1.ReasonRetryLimitExceeded, 0)
		Expect(meta.IsStatusConditionTrue(m.Status.Conditions, flywayv1alpha1.ConditionApplied)).To(BeFalse())

		// Postgres runs the migration in a transaction, so the failed one leaves neither its table nor a success row.
		Expect(psql("SELECT to_regclass('broken.broken')")).To(BeEmpty())
		Expect(psql("SELECT to_regclass('broken.flyway_schema_history')")).To(Equal("broken.flyway_schema_history"))
		Expect(psql("SELECT count(*) FROM broken.flyway_schema_history WHERE version = '1' AND success")).To(Equal("0"))
	})

	It("reports Failed when the database password is wrong", func() {
		retryLimit := 0
		applyMigration("wrong-password", "v1", "wrongpw", "postgres-wrong", "", &retryLimit)

		waitForCondition("wrong-password", flywayv1alpha1.ConditionFailed, flywayv1alpha1.ReasonRetryLimitExceeded, 0)

		Expect(mustRun(exec.Command("kubectl", "-n", namespace, "logs", "-l", "batch.kubernetes.io/job-name=wrong-password",
			"-c", "flyway", "--tail=-1"))).To(ContainSubstring("password authentication failed"))
		Expect(psql("SELECT to_regclass('wrongpw.flyway_schema_history')")).To(BeEmpty())
	})

	It("reports Failed with reason TimedOut when the Job exceeds spec.activeDeadline", func() {
		cmd := exec.Command("kubectl", "-n", namespace, "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: timeout
spec:
  database:
    username: app
    jdbcUrl: jdbc:postgresql://postgres:5432/app
    credentials:
      name: postgres
      key: password
  migrationSource:
    imageRef: %s
  flywayConfiguration:
    defaultSchema: timeout
  retryLimit: 0
  activeDeadline: 45s
`, sqlImage("sleep")))
		mustRun(cmd)

		m := waitForCondition("timeout", flywayv1alpha1.ConditionFailed, flywayv1alpha1.ReasonTimedOut, 0)
		Expect(meta.FindStatusCondition(m.Status.Conditions, flywayv1alpha1.ConditionFailed).Message).
			To(And(ContainSubstring("timed out (activeDeadline 45s)"), ContainSubstring("DeadlineExceeded")))
		Expect(mustRun(exec.Command("kubectl", "-n", namespace, "get", "job", "timeout",
			"-o", "jsonpath={.spec.activeDeadlineSeconds}"))).To(Equal("45"))
	})

	It("sets podMetadata and the operator labels on the Job's pod", func() {
		cmd := exec.Command("kubectl", "-n", namespace, "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: pod-metadata
spec:
  database:
    username: app
    jdbcUrl: jdbc:postgresql://postgres:5432/app
    credentials:
      name: postgres
      key: password
  migrationSource:
    imageRef: %s
  flywayConfiguration:
    defaultSchema: podmetadata
  podMetadata:
    labels:
      example.com/team: payments
    annotations:
      example.com/injected: "false"
`, sqlImage("v1")))
		mustRun(cmd)

		// Any phase will do: the metadata is on the pod from creation.
		Eventually(func(g Gomega) {
			out, err := utils.Run(exec.Command("kubectl", "-n", namespace, "get", "pods",
				"-l", "batch.kubernetes.io/job-name=pod-metadata", "-o", "json"))
			g.Expect(err).NotTo(HaveOccurred())
			var pods struct {
				Items []struct {
					Metadata metav1.ObjectMeta `json:"metadata"`
				} `json:"items"`
			}
			g.Expect(json.Unmarshal([]byte(out), &pods)).To(Succeed())
			g.Expect(pods.Items).NotTo(BeEmpty())
			pod := pods.Items[0].Metadata
			g.Expect(pod.Annotations).To(HaveKeyWithValue("example.com/injected", "false"))
			g.Expect(pod.Labels).To(HaveKeyWithValue("example.com/team", "payments"))
			g.Expect(pod.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "flyway-operator"))
			g.Expect(pod.Labels).To(HaveKeyWithValue(flywayv1alpha1.MigrationLabel, "pod-metadata"))
			g.Expect(pod.Labels).NotTo(HaveKey("app.kubernetes.io/instance"))
		}, jobTimeout, pollInterval).Should(Succeed())
	})
})

// applyMigration creates or updates a Migration against the in-cluster postgres, migrating the given schema.
// An empty image leaves spec.flywayConfiguration.flywayImage unset, so the operator's default applies.
func applyMigration(name, sqlTag, schema, secret, image string, retryLimit *int) {
	GinkgoHelper()
	manifest := fmt.Sprintf(`apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: %s
spec:
  database:
    username: app
    jdbcUrl: jdbc:postgresql://postgres:5432/app
    credentials:
      name: %s
      key: password
  migrationSource:
    imageRef: %s
  flywayConfiguration:
    defaultSchema: %s
`, name, secret, sqlImage(sqlTag), schema)
	if image != "" {
		manifest += fmt.Sprintf("    flywayImage: %s\n", image)
	}
	if retryLimit != nil {
		manifest += fmt.Sprintf("  retryLimit: %d\n", *retryLimit)
	}
	cmd := exec.Command("kubectl", "-n", namespace, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	mustRun(cmd)
}

// waitForCondition waits until condType is True with reason, computed for the Migration's current generation
// (and at least minGeneration), and returns the Migration.
func waitForCondition(name, condType, reason string, minGeneration int64) *flywayv1alpha1.Migration {
	GinkgoHelper()
	var m *flywayv1alpha1.Migration
	Eventually(func(g Gomega) {
		m = getMigration(name)
		g.Expect(m.Generation).To(BeNumerically(">=", minGeneration))
		cond := meta.FindStatusCondition(m.Status.Conditions, condType)
		g.Expect(cond).NotTo(BeNil(), "condition %s not set", condType)
		g.Expect(cond.ObservedGeneration).To(Equal(m.Generation))
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue), "%s: %s", cond.Reason, cond.Message)
		g.Expect(cond.Reason).To(Equal(reason))
	}, jobTimeout, pollInterval).Should(Succeed())
	_, _ = fmt.Fprintf(GinkgoWriter, "migration %s: %s=True reason=%s generation=%d attempts=%d\n",
		name, condType, reason, m.Generation, m.Status.Attempts)
	return m
}

func getMigration(name string) *flywayv1alpha1.Migration {
	GinkgoHelper()
	out := mustRun(exec.Command("kubectl", "-n", namespace, "get", "migration", name, "-o", "json"))
	m := &flywayv1alpha1.Migration{}
	Expect(json.Unmarshal([]byte(out), m)).To(Succeed())
	return m
}

// psql runs query in the postgres pod and returns the unaligned, tuples-only result.
func psql(query string) string {
	GinkgoHelper()
	out := strings.TrimSpace(mustRun(exec.Command("kubectl", "-n", namespace, "exec", "deployment/postgres", "--",
		"psql", "-U", "app", "-d", "app", "-v", "ON_ERROR_STOP=1", "-tAc", query)))
	_, _ = fmt.Fprintf(GinkgoWriter, "psql> %s\n%s\n", query, out)
	return out
}

var unsafeChars = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// dumpDiagnostics writes operator, Migration, Job and pod state to GinkgoWriter and to bin/e2e-logs/<spec>.log.
func dumpDiagnostics(spec string) {
	var b strings.Builder
	run := func(args ...string) {
		out, err := utils.Run(exec.Command("kubectl", args...))
		_, _ = fmt.Fprintf(&b, "\n### kubectl %s\n%s", strings.Join(args, " "), out)
		if err != nil {
			_, _ = fmt.Fprintf(&b, "%v\n", err)
		}
	}
	run("-n", operatorNamespace, "describe", "deployment")
	run("-n", operatorNamespace, "get", "pods", "-o", "wide")
	run("-n", operatorNamespace, "get", "events", "--sort-by=.lastTimestamp")
	run("-n", operatorNamespace, "logs", "-l", "control-plane=controller-manager", "--tail=-1")
	run("-n", namespace, "get", "migrations", "-o", "yaml")
	run("-n", namespace, "get", "jobs,pods", "-o", "wide")
	run("-n", namespace, "get", "events", "--sort-by=.lastTimestamp")
	pods, _ := utils.Run(exec.Command("kubectl", "-n", namespace, "get", "pods",
		"-l", "batch.kubernetes.io/job-name", "-o", "name"))
	for _, pod := range utils.GetNonEmptyLines(pods) {
		run("-n", namespace, "logs", pod, "--all-containers", "--prefix")
	}

	_, _ = fmt.Fprint(GinkgoWriter, b.String())
	dir, err := utils.GetProjectDir()
	if err != nil {
		return
	}
	logDir := filepath.Join(dir, "bin", "e2e-logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(logDir, unsafeChars.ReplaceAllString(spec, "-")+".log"), []byte(b.String()), 0o644)
}
