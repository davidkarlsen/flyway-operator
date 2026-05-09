<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# controller

## Purpose
Reconciliation logic for the `Migration` CRD. The reconciler owns child Kubernetes Jobs (Flyway image) one-per-Migration-generation and mirrors their status back onto `Migration.Status.Conditions`.

## Key Files

| File | Description |
|------|-------------|
| `migration_controller.go` | `MigrationReconciler` (embeds `util.ReconcilerBase` from `redhat-cop/operator-utils`). `Reconcile` drives: fetch → paused-check → look up existing Job by deterministic name → if generation matches mirror status, else build+submit a fresh Job with controller owner reference. `SetupWithManager` registers the controller and watches owned Jobs. |
| `jobutil.go` | Pure builder: turns a `Migration` into a `batchv1.Job` spec. Init container `cp`s SQL files from `MigrationSource.ImageRef` into a shared `emptyDir`; main container runs the Flyway image with env vars derived from `FlywayConfiguration` and `Database`. Annotates the Job with `flyway-operator.davidkarlsen.com/generation`. |
| `suite_test.go` | Ginkgo `TestAPIs` entry point — boots envtest, installs CRDs from `config/crd/bases/`, and starts a manager. Required by all integration specs. |
| `migration_controller_test.go` | Mix of `testing.T`-style table tests using `fake.NewClientBuilder` + `record.FakeRecorder`, and Ginkgo specs that exercise the reconciler against envtest. |
| `migration_controller_integration_test.go` | Ginkgo integration specs running inside `TestAPIs`. |
| `jobutil_test.go` | Pure unit tests for the Job spec builder. |

## For AI Agents

### Working In This Directory
- The reconciler is single-shot per generation: changing the `Migration` spec bumps `metadata.generation`, which deletes/recreates the Job. Do not silently "patch" an existing Job — the generation annotation is the source of truth.
- Honour `IsPaused()` early-exit before any side-effecting calls.
- Always set `controllerutil.SetControllerReference(migration, job, scheme)` so deletion cascades.
- RBAC markers (`+kubebuilder:rbac:groups=...`) on `Reconcile` drive `config/rbac/role.yaml`. After adding a new resource access, run `make manifests`.
- The migration source image **must contain `cp`**; the init container relies on it. If you change the init pattern, document it in `USING.md`.

### Testing Requirements
- Pure unit subset: `go test -count=1 -run "TestCreateJobSpec|TestGithubactionRunnerController" ./internal/controller/`.
- Full suite (Ginkgo + envtest): `make test`. Direct invocation requires `KUBEBUILDER_ASSETS` (set by `make envtest`).
- Tests that need scheme registration call `s.AddKnownTypes(flywayv1alpha1.GroupVersion, migration)` — note `flywayv1alpha1.GroupVersion`, not `flywayv1alpha1.SchemeBuilder.GroupVersion`.

### Common Patterns
- Use `r.GetRecorder()` (from `ReconcilerBase`) to emit events; tests inject `record.NewFakeRecorder(N)`.
- Status updates go through `SetConditions` + `r.UpdateStatus` rather than direct slice mutation.
- Job names and labels are derived deterministically from the `Migration` so reconciles are idempotent.

## Dependencies

### Internal
- `github.com/davidkarlsen/flyway-operator/api/v1alpha1`

### External
- `sigs.k8s.io/controller-runtime/pkg/{client,reconcile,controller,handler,builder,log}`
- `sigs.k8s.io/controller-runtime/pkg/client/fake` (tests)
- `k8s.io/api/{core/v1,batch/v1}`, `k8s.io/apimachinery/pkg/...`
- `k8s.io/client-go/tools/record` — event recording (real + fake)
- `github.com/redhat-cop/operator-utils/pkg/util` — `ReconcilerBase`
- `github.com/onsi/ginkgo/v2`, `github.com/onsi/gomega` — Ginkgo specs
- `github.com/caitlinelfring/go-env-default` — env-var defaulting
- `github.com/samber/lo` — generics utilities

<!-- MANUAL: -->
