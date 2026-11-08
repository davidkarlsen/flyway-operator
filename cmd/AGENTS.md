# cmd

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-08

## Purpose
Operator binary entry point. Wires up the controller-runtime manager, registers the v1alpha1 scheme, sets up health/metrics endpoints and leader election, detects whether the cluster is OpenShift, starts `MigrationReconciler` (with a typed core client as its `LogReader` for pod logs) and registers the `MigrationCollector` metrics collector.

## Key Files

| File | Description |
|------|-------------|
| `main.go` | Manager bootstrap; flags `--metrics-bind-address` (default `0`, the manifests set `:8443`), `--metrics-secure`, `--metrics-cert-path`/`-name`/`-key`, `--health-probe-bind-address`, `--leader-elect`; scheme registration via `flywayv1alpha1.AddToScheme`; `controller.IsOpenShift` detection passed to the reconciler as `OpenShift`; controller setup; `metrics.Registry.MustRegister(controller.NewMigrationCollector(mgr.GetCache(), mgr.Elected()))`. |

## For AI Agents

### Working In This Directory
- Keep `main.go` thin — reconciliation logic belongs in `internal/controller/`.
- New reconcilers must be registered here via `SetupWithManager(mgr)` after the manager is constructed.
- When adding a new API version, also add `<version>.AddToScheme(scheme)` here.

### Testing Requirements
- `main.go` has no tests of its own; the reconciler is covered by `make test` (envtest) and the whole operator by `make test-e2e` (kind). Flag changes must be mirrored in `config/default/manager_metrics_patch.yaml`, `config/manager/manager.yaml` and the Helm `config/helm-chart/flyway-operator/templates/deployment.yaml`, and documented in `docs/metrics.md`.

## Dependencies

### Internal
- `github.com/davidkarlsen/flyway-operator/api/v1alpha1`
- `github.com/davidkarlsen/flyway-operator/internal/controller`

### External
- `sigs.k8s.io/controller-runtime` — manager, signal handler, leader election
- `k8s.io/client-go/kubernetes/scheme` — base scheme
- `sigs.k8s.io/controller-runtime/pkg/metrics` — registry and secured server filters

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
