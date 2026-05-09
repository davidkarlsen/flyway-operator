<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# flyway-operator

## Purpose
Kubernetes operator that reconciles `Migration` CRDs (`flyway.davidkarlsen.com/v1alpha1`) by spawning Kubernetes Jobs which run the [Flyway](https://flywaydb.org/) Docker image to apply database migrations. Built with Kubebuilder v4 (`go.kubebuilder.io/v4`) on top of `controller-runtime`. Module path: `github.com/davidkarlsen/flyway-operator`.

## Key Files

| File | Description |
|------|-------------|
| `Makefile` | Canonical build/test/release entry points; auto-downloads `controller-gen`, `kustomize`, `envtest`, `operator-sdk`, `opm` into `./bin/` |
| `PROJECT` | Kubebuilder project descriptor (domain, layout, tracked resources) — generated, do not hand-edit |
| `go.mod` / `go.sum` | Go module + dependency graph |
| `Dockerfile` | Operator image build |
| `Dockerfile.catalog` / `bundle.Dockerfile` / `catalog-template.yaml` | OLM catalog/bundle build assets |
| `kind-config.yaml` / `ct-config.yaml` / `codecov.yml` | Kind cluster + chart-testing + Codecov config |
| `CLAUDE.md` | AI agent guidance (commands, architecture, conventions) |
| `README.md` / `INSTALLING.md` / `USING.md` / `CONTRIBUTING.md` | Human-facing docs |

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `api/` | CRD Go types and scheme registration (see `api/AGENTS.md`) |
| `cmd/` | Operator binary entry point (see `cmd/AGENTS.md`) |
| `internal/` | Reconciler implementation, kept private to module (see `internal/AGENTS.md`) |
| `config/` | Kustomize bases (CRD, RBAC, manager, default, samples, prometheus, scorecard) and the Helm chart (see `config/AGENTS.md`) |
| `bundle/` | OLM bundle manifests, metadata, scorecard tests (see `bundle/AGENTS.md`) |
| `hack/` | License header boilerplate consumed by `controller-gen` (see `hack/AGENTS.md`) |
| `.github/` | GitHub Actions workflows + Copilot rules |

## For AI Agents

### Working In This Directory
- Run `make manifests fmt vet` before opening a PR — CI runs `golangci-lint` (default config; no `.golangci.*` in repo) plus `make test`.
- After editing API types, run `make manifests generate` and update `config/samples/` if the public shape changed.
- Operator-owned annotations must use the `Prefix = "flyway-operator.davidkarlsen.com"` constant from `api/v1alpha1/migration_types.go` — do not hardcode the domain.
- Apache 2.0 license header (see `hack/boilerplate.go.txt`) is required on new Go files; `controller-gen` enforces it.

### Testing Requirements
- Full suite: `make test` (provisions envtest assets via `setup-envtest` and exports `KUBEBUILDER_ASSETS`).
- Pure unit tests (no envtest): `go test -count=1 -run "TestCreateJobSpec|TestGithubactionRunnerController" ./internal/controller/`.
- Single Ginkgo spec: `go test -v -count=1 -run "TestAPIs$" -ginkgo.focus="<regex>" ./internal/controller/` (after `make envtest` and exporting `KUBEBUILDER_ASSETS`).

### Common Patterns
- Conditions are the canonical status mechanism — update them via `SetConditions`, not by mutating the slice in place.
- Env-var defaults use `github.com/caitlinelfring/go-env-default`.
- Scheme registration uses upstream `k8s.io/apimachinery/pkg/runtime.SchemeBuilder` (NOT the deprecated `sigs.k8s.io/controller-runtime/pkg/scheme.Builder`); types are registered inside `addKnownTypes`, not via `init()` in `migration_types.go`.

### CI
- `.github/workflows/build.yaml` — main lint/test/release pipeline.
- `.github/workflows/chart-test.yaml` — Helm `ct` against Kind.
- `.github/workflows/codeql.yaml` — CodeQL security scan.
- Dependabot opens dep-bump PRs; when controller-runtime / k8s.io bumps trigger lint regressions (e.g. `SA1019`), fix the deprecation in the same or a replacement PR rather than pinning lint.

## Dependencies

### External (key)
- `sigs.k8s.io/controller-runtime` — controller framework
- `k8s.io/{api,apimachinery,client-go}` v0.35.x — Kubernetes types and client
- `github.com/onsi/ginkgo/v2` + `github.com/onsi/gomega` — BDD test framework
- `github.com/redhat-cop/operator-utils` — `ReconcilerBase`, condition helpers
- `github.com/samber/lo` — Go generics utilities
- `github.com/caitlinelfring/go-env-default` — env-var defaulting

<!-- MANUAL: Custom project notes can be added below -->
