# grafana

**Parent context:** `../AGENTS.md`
**Generated:** 2026-11-08 · **Updated:** 2026-11-08

## Purpose
Grafana dashboards for the operator's metrics. A kustomize `configMapGenerator` packs them into the `flyway-operator-dashboards` ConfigMap (label `grafana_dashboard: "1"`, no name-suffix hash), included by `default/` and so shipped in the OLM bundle.

## Key Files

| File | Description |
|------|-------------|
| `dashboards/flyway-operator.json` | Operator health dashboard (uid `flyway-operator`), for the platform team |
| `dashboards/flyway-migrations.json` | Migration status and runs dashboard (uid `flyway-migrations`), for workload teams |
| `kustomization.yaml` | `configMapGenerator`, one key per dashboard file |

## For AI Agents

### Working In This Directory
- The JSON files are the source of truth. Use only metrics listed in `docs/metrics.md`, keep the panels that mirror alerts in step with the alert expressions there, and keep the `${datasource}` variable instead of a datasource UID.
- After a change, check that `kustomize build config/manifests` still emits the ConfigMap, and regenerate its bundle file with `make bundle`. See `docs/dashboards.md`.

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
