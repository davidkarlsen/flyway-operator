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

// Package utils holds helpers for the e2e tests in test/e2e.
package utils

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/caitlinelfring/go-env-default"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot-import is the Ginkgo convention
)

// Run executes cmd from the project root and returns its stdout. On failure the error includes stderr.
func Run(cmd *exec.Cmd) (string, error) {
	dir, err := GetProjectDir()
	if err != nil {
		return "", err
	}
	cmd.Dir = dir

	command := strings.Join(cmd.Args, " ")
	_, _ = fmt.Fprintf(GinkgoWriter, "running: %s\n", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s failed with error: (%v) %s%s", command, err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// LoadImageToKindCluster loads a local docker image into the kind cluster named by $KIND_CLUSTER.
// `kind load docker-image` fails for multi-platform images under docker's containerd image store,
// so on failure the image is saved for the docker server's platform and loaded as an archive.
func LoadImageToKindCluster(name string) error {
	kind := env.GetDefault("KIND", "kind")
	cluster := env.GetDefault("KIND_CLUSTER", "flyway-operator-e2e")
	if _, loadErr := Run(exec.Command(kind, "load", "docker-image", name, "--name", cluster)); loadErr != nil {
		if err := loadImageArchive(kind, cluster, name); err != nil {
			return fmt.Errorf("%w; fallback via docker save failed: %w", loadErr, err)
		}
	}
	return nil
}

func loadImageArchive(kind, cluster, name string) error {
	platform, err := Run(exec.Command("docker", "version", "--format", "{{.Server.Os}}/{{.Server.Arch}}"))
	if err != nil {
		return err
	}
	archive, err := os.CreateTemp("", "kind-image-*.tar")
	if err != nil {
		return err
	}
	_ = archive.Close()
	defer func() { _ = os.Remove(archive.Name()) }()

	if _, err := Run(exec.Command("docker", "save", "--platform", strings.TrimSpace(platform), "-o", archive.Name(), name)); err != nil {
		return err
	}
	_, err = Run(exec.Command(kind, "load", "image-archive", archive.Name(), "--name", cluster))
	return err
}

// GetNonEmptyLines splits output into lines and drops the empty ones.
func GetNonEmptyLines(output string) []string {
	var res []string
	for _, line := range strings.Split(output, "\n") {
		if line != "" {
			res = append(res, line)
		}
	}
	return res
}

// GetProjectDir returns the repository root.
func GetProjectDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return wd, err
	}
	return strings.TrimSuffix(wd, "/test/e2e"), nil
}
