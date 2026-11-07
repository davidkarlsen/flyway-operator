# default

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
The default kustomize overlay rendered by `make deploy`. Composes the CRD, manager Deployment, RBAC, `config/prometheus` (the `ServiceMonitor`) and metrics service into a single applyable bundle, applying the `IMG` substitution.

## Key Files

| File | Description |
|------|-------------|
| `kustomization.yaml` | Top-level overlay — sets the namespace (`flyway-operator-system`), name prefix, image override, and resources list (`../crd`, `../rbac`, `../manager`, `../prometheus`, `metrics_service.yaml`) |
| `manager_metrics_patch.yaml` | JSON6902 patch adding `--metrics-bind-address=:8443` to the manager args |
| `metrics_service.yaml` | `Service` exposing the controller's metrics port |

## For AI Agents

### Working In This Directory
- The `IMG` env var passed to `make deploy` is substituted via `kustomize edit set image` against this overlay.
- The `ServiceMonitor` is always applied, so the cluster needs the Prometheus Operator CRDs; see `docs/metrics.md`.
- Adding a new manifest to the deployed bundle? Reference it from `kustomization.yaml`'s `resources:` list.

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
