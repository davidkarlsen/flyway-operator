# controller

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
Reconciliation logic for the `Migration` CRD. The reconciler owns child Kubernetes Jobs (Flyway image) one-per-Migration-generation and mirrors their status back onto `Migration.Status.Conditions`.

## Key Files

| File | Description |
|------|-------------|
| `migration_controller.go` | `MigrationReconciler` (embeds `util.ReconcilerBase` from `redhat-cop/operator-utils`; `OpenShift` field set from `IsOpenShift` at startup). `Reconcile` drives: fetch, paused-check, look up the existing Job by deterministic name. A Job of an older generation is waited for while running (`PreviousJobRunning`), otherwise deleted (`PreviousJobReplaced`), after recording `status.lastAppliedImageRef` from a succeeded one. A current Job is mirrored into the `Applied`/`Progressing`/`Failed` conditions; a failed one is deleted and re-created (attempt annotation +1) until `spec.retryLimit`, then left with `Failed=True` (`RetryLimitExceeded`, or `TimedOut` when the Job hit its `activeDeadlineSeconds`). The status is computed also while a Job is terminating (`deleteJob` only requeues), so `Retrying`/`PreviousJobReplaced` survive a lost status write. No Job: create the next attempt (`submitMigrationJob`). `rejectJobWithoutImageVolume` handles a Job the API server stored without the image volume. `SetupWithManager` registers the controller and watches owned Jobs. |
| `jobutil.go` | Builder and helpers: `createJobSpec` turns a `Migration` into a `batchv1.Job`. It mounts `MigrationSource.ImageRef` read-only as an image volume (`volumes[].image`) at `/flyway/migration-source`; the Flyway container reads it via `FLYWAY_LOCATIONS=filesystem:<mount>/<path>` with `FLYWAY_FAIL_ON_MISSING_LOCATIONS=true`, each skipped when the user `envVars` set the same name. The Flyway container runs the Flyway image (`getFlywayImage`: spec, then the operator's `FLYWAY_IMAGE` env var via `go-env-default`, then `docker.io/flyway/flyway:13`) with env vars derived from `FlywayConfiguration` and `Database`. It sets `activeDeadlineSeconds` from `spec.activeDeadline`, merges `jobMetadata`/`podMetadata` with the operator's labels (`withOperatorKeys`, operator keys win) and annotates the Job with `flyway-operator.davidkarlsen.com/generation` and `/attempt`. `setSecurityContext` applies the Pod Security "restricted" profile (non-root, RuntimeDefault seccomp, no privilege escalation, all capabilities dropped, read-only root filesystem, a writable `emptyDir` at `/tmp`); `spec.flywayConfiguration.podSecurityContext`/`securityContext` replace the defaults, and outside OpenShift the pod runs as UID/fsGroup 1001 (`IsOpenShift` leaves it to the SCC). `hasSourceImageVolume` (volume name + `Image != nil`, reference not compared) detects a Job stored without the volume (ImageVolume gate off); the reconciler then deletes it and sets `Failed` with reason `ImageVolumeUnsupported` until the spec changes. Also `jobState` (reads the Job's `Complete`/`Failed` conditions, never pod counts), `jobSourceImage`/`setLastApplied` (a Job from an operator before 0.6.0 still names its `copy-sql` init container, which is read only to recover its source image) and the condition setters. |
| `metrics.go` | Prometheus metrics: `flyway_operator_migration_runs_total` and `..._last_run_duration_seconds`, recorded once per finished attempt (`outcomeIsNew` compares with the previous status so a repeated reconcile or lost write does not double-count); `MigrationCollector` reports the condition, last-transition and paused series at scrape time from the manager cache, only after leader election. See `docs/metrics.md`. |
| `suite_test.go` | Ginkgo `TestAPIs` entry point: boots envtest, installs CRDs from `config/crd/bases/`, and starts a manager. Required by the Ginkgo specs. |
| `migration_controller_test.go` | `TestGithubactionRunnerController`, a plain `testing.T` test of the reconciler with the fake client. |
| `migration_controller_integration_test.go` | Ginkgo integration specs against envtest, running inside `TestAPIs`. |
| `docs_examples_test.go` | Ginkgo specs inside `TestAPIs` that apply every `kind: Migration` YAML block of `docs/*.md`, `docs/examples/*.md` and the `config/samples` sample to the real CRD, so documented examples must stay valid. It also requires one reference `Migration` named like the OLM sample, plus any number of per-use-case examples with their own names in `docs/usage.md`, and the OLM sample to be a subset of the reference. |
| `migration_status_test.go` | Fake-client tests of the status contract (`TestStatus*`): running, succeeded, retry under the limit, stop at the limit, spec change, deadline, last applied image, dropped image volume, legacy `copy-sql` Jobs, terminating Jobs. |
| `metrics_test.go` | `TestMetrics*`, `TestOutcomeIsNew`, `TestMigrationCollector`: counting, collector output, cleanup on delete. |
| `jobutil_test.go` | Pure unit tests for the Job spec builder, security context, `IsOpenShift`, source location and image volume detection. |

## For AI Agents

### Working In This Directory
- The reconciler is single-shot per generation: changing the `Migration` spec bumps `metadata.generation`; the Job of the older generation is waited for while it is still running (`PreviousJobRunning`) and deleted once finished (`PreviousJobReplaced`), then the next generation's Job is created. Do not silently "patch" an existing Job; the generation annotation is the source of truth.
- Honour `IsPaused()` early-exit before any side-effecting calls. Metrics bookkeeping is the exception: `initRunMetrics` (and `deleteRunMetrics` on deletion or not-found) runs before the paused check, so a paused Migration still has its run counters at 0.
- Always set `controllerutil.SetControllerReference(migration, job, scheme)` so deletion cascades.
- RBAC markers (`+kubebuilder:rbac:groups=...`) on `Reconcile` drive `config/rbac/role.yaml`. After adding a new resource access, run `make manifests`.
- Every status path goes through `setAll`/`setConditions`/`setApplied`/`setFailed`, which set all three conditions with one reason. A new reason needs a `Reason*` const in `api/v1alpha1/migration_types.go`, a mention in `docs/usage.md` and a status test.
- Metrics are recorded only after the status write succeeded (see `outcomeIsNew`); keep new outcomes behind it so they are counted once.
- The migration source image is an image volume (needs Kubernetes 1.35+ and containerd 2.1+ / CRI-O 1.31+); it needs no shell or tools. If you change how it is consumed, document it in `docs/usage.md`.
- Changes to the Job spec must stay Pod Security "restricted" compliant (every container gets the security context and the `/tmp` mount in `setSecurityContext`).

### Testing Requirements
- Pure unit subset: `go test -count=1 -run "TestCreateJobSpec|TestGithubactionRunnerController|TestStatus|TestMetrics" ./internal/controller/`.
- Full suite (Ginkgo + envtest): `make test`. Direct invocation requires `KUBEBUILDER_ASSETS` (set by `make envtest`).
- Tests that need scheme registration call `s.AddKnownTypes(flywayv1alpha1.GroupVersion, migration)`; note `flywayv1alpha1.GroupVersion`, not `flywayv1alpha1.SchemeBuilder.GroupVersion`.
- Anything a real cluster would show (Job behaviour, image volumes, retries) also needs the kind e2e suite in `test/e2e/` (`make test-e2e`).

### Common Patterns
- Use `r.GetRecorder()` (from `ReconcilerBase`) to emit events; tests inject `record.NewFakeRecorder(N)`.
- Status updates go through the `set*` helpers and `r.ManageSuccess` / `r.ManageError` / `r.ManageSuccessWithRequeue` rather than direct slice mutation.
- Job names and labels are derived deterministically from the `Migration` so reconciles are idempotent.

## Dependencies

### Internal
- `github.com/davidkarlsen/flyway-operator/api/v1alpha1`

### External
- `sigs.k8s.io/controller-runtime/pkg/{client,reconcile,controllerutil,log,metrics}`
- `sigs.k8s.io/controller-runtime/pkg/client/fake` (tests)
- `k8s.io/api/{core/v1,batch/v1}`, `k8s.io/apimachinery/pkg/...`
- `k8s.io/client-go/tools/record` — event recording (real + fake)
- `github.com/redhat-cop/operator-utils/pkg/util` — `ReconcilerBase`
- `github.com/onsi/ginkgo/v2`, `github.com/onsi/gomega` — Ginkgo specs
- `github.com/caitlinelfring/go-env-default` — env-var defaulting
- `github.com/prometheus/client_golang` — metrics
- `github.com/samber/lo` — generics utilities

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
