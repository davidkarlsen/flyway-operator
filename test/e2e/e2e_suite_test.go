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
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/caitlinelfring/go-env-default"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/davidkarlsen/flyway-operator/internal/controller"
	"github.com/davidkarlsen/flyway-operator/test/utils"
)

const (
	namespace         = "flyway-e2e"
	operatorNamespace = "flyway-operator-system"
	operatorImage     = "flyway-operator:e2e"
	postgresImage     = "docker.io/library/postgres:17-alpine"
	// flywayImagePlaceholder in test/e2e/config is replaced by flywayImage, the operator's FLYWAY_IMAGE.
	flywayImagePlaceholder = "E2E_FLYWAY_IMAGE"
)

var (
	// registry is the local registry the kind node pulls from, see setup-kind.sh.
	registry = env.GetDefault("E2E_REGISTRY", "localhost:5001")
	// flywayImage mirrors the operator's default Flyway image into the local registry. The operator pulls it with
	// imagePullPolicy Always, so a `kind load`ed image would not be used.
	flywayImage = registry + "/" + strings.TrimPrefix(controller.DefaultFlywayImage, "docker.io/")
	serviceMon  = regexp.MustCompile(`(?m)^kind: ServiceMonitor$`)
	// setupComplete is false when BeforeSuite failed, so AfterSuite dumps diagnostics.
	setupComplete bool
)

func sqlImage(tag string) string {
	return registry + "/flyway-e2e-sql:" + tag
}

// TestE2E runs the e2e suite against the kind cluster created by `make test-e2e`.
func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	_, _ = fmt.Fprintf(GinkgoWriter, "Starting flyway-operator e2e suite\n")
	RunSpecs(t, "e2e suite")
}

var _ = BeforeSuite(func() {
	By("removing the test namespace left by an aborted run")
	mustRun(exec.Command("kubectl", "delete", "namespace", namespace, "--ignore-not-found", "--timeout=3m"))

	By("building and loading the operator image")
	mustRun(exec.Command("docker", "build", "-t", operatorImage, "."))
	Expect(utils.LoadImageToKindCluster(operatorImage)).To(Succeed())

	By("pulling and loading the postgres image")
	mustRun(exec.Command("docker", "pull", postgresImage))
	Expect(utils.LoadImageToKindCluster(postgresImage)).To(Succeed())

	By("building and pushing the migration-source images")
	for _, tag := range []string{"v1", "v2", "broken", "sleep"} {
		mustRun(exec.Command("docker", "build", "-t", sqlImage(tag),
			"-f", "test/e2e/testdata/Dockerfile.sql", "test/e2e/testdata/sql-"+tag))
		mustRun(exec.Command("docker", "push", sqlImage(tag)))
	}

	By("mirroring the flyway image into the local registry")
	// Building FROM the upstream image yields a single-platform image, which pushes under any docker image store.
	build := exec.Command("docker", "build", "-t", flywayImage, "-")
	build.Stdin = strings.NewReader("FROM " + controller.DefaultFlywayImage + "\n")
	mustRun(build)
	mustRun(exec.Command("docker", "push", flywayImage))

	By("deploying the operator")
	apply := exec.Command("kubectl", "apply", "-f", "-")
	apply.Stdin = strings.NewReader(operatorManifests())
	mustRun(apply)
	mustRun(exec.Command("kubectl", "-n", operatorNamespace, "rollout", "status",
		"deployment/flyway-operator-controller-manager", "--timeout=3m"))

	By("deploying postgres")
	ns := exec.Command("kubectl", "apply", "-f", "-")
	ns.Stdin = strings.NewReader("apiVersion: v1\nkind: Namespace\nmetadata:\n  name: " + namespace + "\n")
	mustRun(ns)
	mustRun(exec.Command("kubectl", "-n", namespace, "apply", "-f", "test/e2e/testdata/postgres.yaml"))
	mustRun(exec.Command("kubectl", "-n", namespace, "rollout", "status", "deployment/postgres", "--timeout=3m"))
	setupComplete = true
})

var _ = AfterSuite(func() {
	if !setupComplete {
		dumpDiagnostics("BeforeSuite")
	}
	By("removing the test namespace and the operator")
	_, _ = utils.Run(exec.Command("kubectl", "delete", "namespace", namespace, "--ignore-not-found", "--timeout=2m"))
	del := exec.Command("kubectl", "delete", "--ignore-not-found", "-f", "-")
	del.Stdin = strings.NewReader(operatorManifests())
	_, _ = utils.Run(del)
})

// operatorManifests renders test/e2e/config. ServiceMonitors are dropped: kind has no prometheus-operator CRDs.
func operatorManifests() string {
	out := mustRun(exec.Command(env.GetDefault("KUSTOMIZE", "kustomize"), "build", "test/e2e/config"))
	var docs []string
	for _, doc := range strings.Split(out, "\n---\n") {
		if !serviceMon.MatchString(doc) {
			docs = append(docs, doc)
		}
	}
	return strings.ReplaceAll(strings.Join(docs, "\n---\n"), flywayImagePlaceholder, flywayImage)
}

func mustRun(cmd *exec.Cmd) string {
	GinkgoHelper()
	out, err := utils.Run(cmd)
	Expect(err).NotTo(HaveOccurred())
	return out
}
