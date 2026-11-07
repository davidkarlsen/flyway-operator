# prometheus

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
Kustomize component adding a `ServiceMonitor` so Prometheus Operator scrapes the manager's metrics service.

## Key Files

| File | Description |
|------|-------------|
| `monitor.yaml` | `ServiceMonitor` targeting the metrics Service from `config/default/metrics_service.yaml` |
| `kustomization.yaml` | Kustomize wiring |

## For AI Agents

### Working In This Directory
- `config/default/kustomization.yaml` includes this component (`../prometheus`), so `make deploy` needs the `monitoring.coreos.com/v1` CRDs. On clusters without the Prometheus Operator, remove `- ../prometheus` from that file first (see `docs/metrics.md`).

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
