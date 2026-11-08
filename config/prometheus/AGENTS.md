# prometheus

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
Kustomize component adding a `ServiceMonitor` so Prometheus Operator scrapes the manager's metrics service, and the alert `PrometheusRule`.

## Key Files

| File | Description |
|------|-------------|
| `monitor.yaml` | `ServiceMonitor` targeting the metrics Service from `config/default/metrics_service.yaml` |
| `rules.yaml` | `PrometheusRule` with the operator and Migration alerts; the chart's `files/rules.yaml` is a copy of its groups, refreshed with `make helm-rules` in the chart release PR (not in the alert change, since merging a chart change publishes the chart); chart CI fails on chart PRs if it is stale |
| `tests/rules_test.yaml` | `promtool test rules` unit tests for the alerts; run with `make test-rules` |
| `kustomization.yaml` | Kustomize wiring |

## For AI Agents

### Working In This Directory
- `config/default/kustomization.yaml` includes this component (`../prometheus`), so `make deploy` needs the `monitoring.coreos.com/v1` CRDs. On clusters without the Prometheus Operator, remove `- ../prometheus` from that file first (see `docs/metrics.md`); the same applies to `rules.yaml`.

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
