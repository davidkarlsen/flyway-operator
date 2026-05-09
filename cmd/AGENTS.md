<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# cmd

## Purpose
Operator binary entry point. Wires up the controller-runtime manager, registers the v1alpha1 scheme, sets up health/metrics endpoints and leader election, and starts `MigrationReconciler`.

## Key Files

| File | Description |
|------|-------------|
| `main.go` | Manager bootstrap; flag parsing for metrics-bind-address, health-probe-bind-address, leader-elect; scheme registration via `flywayv1alpha1.AddToScheme`; controller setup. |

## For AI Agents

### Working In This Directory
- Keep `main.go` thin — reconciliation logic belongs in `internal/controller/`.
- New reconcilers must be registered here via `SetupWithManager(mgr)` after the manager is constructed.
- When adding a new API version, also add `<version>.AddToScheme(scheme)` here.

### Testing Requirements
- Smoke-tested implicitly through `make test` (envtest spins up a manager-equivalent).

## Dependencies

### Internal
- `github.com/davidkarlsen/flyway-operator/api/v1alpha1`
- `github.com/davidkarlsen/flyway-operator/internal/controller`

### External
- `sigs.k8s.io/controller-runtime` — manager, signal handler, leader election
- `k8s.io/client-go/kubernetes/scheme` — base scheme

<!-- MANUAL: -->
