# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Kubernetes operator that reconciles a `Migration` CRD (`flyway.davidkarlsen.com/v1alpha1`) by spawning Kubernetes Jobs which run the Flyway Docker image to apply database migrations. Built with Kubebuilder v4 (`go.kubebuilder.io/v4`) on top of `controller-runtime`. Module path: `github.com/davidkarlsen/flyway-operator`.

## Common commands

All commands are driven through the `Makefile`; tools (`controller-gen`, `kustomize`, `envtest`, `operator-sdk`, `opm`) are auto-downloaded into `./bin/` on first use.

| Task | Command |
|------|---------|
| Generate CRDs / RBAC from kubebuilder markers | `make manifests` |
| Generate `zz_generated.deepcopy.go` | `make generate` |
| Format / vet | `make fmt` / `make vet` |
| Full build (runs manifests, generate, fmt, vet) | `make build` |
| Run all tests with envtest | `make test` |
| Run operator locally against current kubeconfig | `make run` |
| Install CRDs into the cluster | `make install` |
| Build container image | `make docker-build IMG=<registry>/flyway-operator:tag` |
| Deploy / undeploy via kustomize | `make deploy IMG=...` / `make undeploy` |
| OLM bundle | `make bundle` / `make bundle-build` / `make bundle-push` |

Pre-PR checklist (from CONTRIBUTING.md and `.github/copilot-instructions.md`): run `make manifests fmt vet` before opening a PR. CI also runs `golangci-lint` (default config — there is no repo-level `.golangci.*`).

### Running a single test

The controller package mixes plain `testing.T` tests with a Ginkgo suite (`suite_test.go` registers `TestAPIs`, which boots envtest):

```sh
# pure unit tests (no envtest needed):
go test -count=1 -run "TestCreateJobSpec|TestGithubactionRunnerController" ./internal/controller/

# Ginkgo specs inside TestAPIs (requires envtest binaries — `make envtest` first, then export KUBEBUILDER_ASSETS):
go test -v -count=1 -run "TestAPIs$" -ginkgo.focus="<spec name regex>" ./internal/controller/
```

`make test` is the canonical way to run the full suite — it provisions envtest assets via `setup-envtest` and exports `KUBEBUILDER_ASSETS` for the test process.

## Architecture

### CRD shape (`api/v1alpha1/migration_types.go`)

A `Migration` carries three sub-specs:

- `Database` — `username` + `credentials` (Kubernetes `SecretKeySelector`) + `jdbcUrl` (validated by regex `^jdbc:.*`).
- `FlywayConfiguration` — Flyway-specific knobs (baseline, default schema, placeholders, etc.) propagated to the Job as env vars.
- `MigrationSource` — an `imageRef` mounted read-only into the Flyway container as an image volume (`volumes[].image`, Kubernetes 1.35+), so the image needs no shell and can be `FROM scratch`; Flyway reads it through `FLYWAY_LOCATIONS=filesystem:/flyway/migration-source/<path>` (user `envVars` can override it). If the API server drops the image volume (ImageVolume gate off), the Job is deleted and the Migration reports `Failed` / `ImageVolumeUnsupported`. Optional `flywayImage` override (defaults to `docker.io/flyway/flyway:13`), `path` (default `/sql`), and `encoding`.

Status is tracked entirely through `metav1.Condition`s on `MigrationStatus.Conditions`. Helper methods `GetConditions` / `SetConditions` satisfy the `redhat-cop/operator-utils` `ReconcilerBase` interface.

Operator-owned annotations are namespaced under the `Prefix = "flyway-operator.davidkarlsen.com"` constant. Two are used today:
- `flyway-operator.davidkarlsen.com/generation` — written onto the spawned Job to track which `Migration.Generation` produced it (`GenerationAsString()`).
- `flyway-operator.davidkarlsen.com/paused` — when set to `"true"` on a `Migration`, `IsPaused()` returns true and reconciliation is short-circuited.

### Reconcile loop (`internal/controller/migration_controller.go`)

`MigrationReconciler.Reconcile` (single entry point) does roughly:

1. Fetch the `Migration`; honour `IsPaused()` early-exit.
2. Look up an existing owned Job by deterministic name (`getExistingJob`).
3. If the Job exists and its `generation` annotation matches the current `Migration.Generation`, mirror its result into the `Applied`/`Progressing`/`Failed` conditions (with `observedGeneration`); re-create a failed Job until `spec.retryLimit`, then stop with `Failed=True`. A Job of an older generation is deleted once finished.
4. Otherwise build a fresh Job spec via `internal/controller/jobutil.go` (SQL image mounted as an image volume; Flyway container runs with env vars derived from `FlywayConfiguration` and `Database`) and submit it (`submitMigrationJob`), with the Migration set as controller owner reference so deletion cascades.

The reconciler embeds `util.ReconcilerBase` from `redhat-cop/operator-utils` and uses its condition / event helpers. Events go to a `record.EventRecorder` (real one in `cmd/main.go`, `record.FakeRecorder` in tests).

### Scheme registration (`api/v1alpha1/groupversion_info.go`)

Uses the upstream `k8s.io/apimachinery/pkg/runtime.SchemeBuilder` pattern, **not** the deprecated `sigs.k8s.io/controller-runtime/pkg/scheme.Builder`. Types are registered inside `addKnownTypes`, not via an `init()` in `migration_types.go`. The exported package symbol for the GroupVersion is `flywayv1alpha1.GroupVersion` — there is no `SchemeBuilder.GroupVersion` field on the new builder type.

### Layout summary

| Path | What |
|------|------|
| `api/v1alpha1/` | CRD types, kubebuilder markers, deepcopy, scheme registration |
| `cmd/main.go` | Manager bootstrap, leader election, controller registration |
| `internal/controller/` | `MigrationReconciler` + Job spec builder + tests |
| `config/` | Kustomize bases (CRDs, RBAC, manager, default overlay, samples, prometheus, scorecard, manifests for OLM) |
| `config/helm-chart/flyway-operator/` | Helm chart published to `gh-pages` |
| `bundle/` + `bundle.Dockerfile` + `catalog-template.yaml` + `Dockerfile.catalog` | OLM bundle and catalog assets |
| `hack/boilerplate.go.txt` | License header injected by `controller-gen` |

## Conventions

From `.github/copilot-instructions.md` (Copilot rules):

- Apache 2.0 license header (see `hack/boilerplate.go.txt`) on new Go files; controller-gen also enforces it.
- Use kubebuilder validation markers (`+kubebuilder:validation:Required|Optional|Pattern`, `+kubebuilder:default`) for all spec fields. After editing API types, run `make manifests generate` and update the examples in `docs/usage.md` if the public shape changed. `config/samples/` is only the OLM `alm-examples` source; keep it minimal and valid, not a second set of docs.
- Operator-specific annotations must use the `Prefix` constant — do not hardcode the domain.
- Env-var defaults use `github.com/caitlinelfring/go-env-default`.
- Conditions are the canonical status mechanism; update them via `SetConditions` rather than mutating the slice in place.

## Workflows (skills and agents)

Repo workflows live in `.claude/skills/` and build on the oh-my-claudecode agents; they hold the repo-specific rules, so read the relevant one before acting:

| Skill | Use for |
|---|---|
| `issue-to-pr` | feature/bug from issue to a PR that closes it (design review, worktree, tests incl. kind e2e lock, PR conventions, model selection) |
| `review-loop` | independent critic + code-reviewer rounds until approval, then the merge rules |
| `operator-release` | readiness check, tag-build verification, Helm chart release PR, post-release gate |
| `tooling-upgrade` | one PR per outdated tool (Go, operator-sdk, controller-tools, envtest, kustomize, opm, default Flyway image) |
| `security-sweep` | govulncheck, Dependabot/code-scanning alerts, Scorecard |

The skills assume the oh-my-claudecode (OMC) plugin for the `critic`, `code-reviewer`, `architect`, `executor`, `plan`, `autopilot` and `ralph` agents and skills. Without OMC, use the built-in `general-purpose` agent with the same instructions.

Agents in `.claude/agents/`: `operator-developer` (implements issues) and `operator-release-manager` (releases, upgrades, security). Reviews reuse the OMC `critic`, `code-reviewer` and `architect` agents.

## CI

GitHub Actions in `.github/workflows/`:
- `build.yaml` — main lint/test/release pipeline (uses `golangci/golangci-lint-action` with `--timeout=10m`, then `make test`, then container/helm publish).
- `chart-test.yaml` — Helm chart-testing (`ct`) using `ct-config.yaml` and `kind-config.yaml`.
- `codeql.yaml` — CodeQL security scan.

Dependabot opens dep-bump PRs; when a controller-runtime / k8s.io bump introduces lint regressions (e.g. `SA1019` deprecation), fix the deprecation in the same or a replacement PR rather than pinning the lint rule.
