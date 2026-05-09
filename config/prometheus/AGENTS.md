<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# prometheus

## Purpose
Optional kustomize component adding a `ServiceMonitor` so Prometheus Operator scrapes the manager's metrics service.

## Key Files

| File | Description |
|------|-------------|
| `monitor.yaml` | `ServiceMonitor` targeting the metrics Service from `config/default/metrics_service.yaml` |
| `kustomization.yaml` | Kustomize wiring |

## For AI Agents

### Working In This Directory
- This component is opt-in — enable it from `config/default/kustomization.yaml` (or a downstream overlay) when deploying to a cluster running Prometheus Operator.

<!-- MANUAL: -->
