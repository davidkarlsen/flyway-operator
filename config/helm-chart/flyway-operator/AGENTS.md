# flyway-operator

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-08

## Purpose
Helm chart for installing the operator outside of OLM. Mirrors the deployment surface in `config/manager/` and `config/rbac/` but expressed as Go templates.

## Key Files

| File | Description |
|------|-------------|
| `Chart.yaml` | Chart metadata (name, `version`, `appVersion`, `kubeVersion: ">=1.35.0-0"`) |
| `Chart.lock` | Pinned dependency versions |
| `values.yaml` | User-facing knobs (image, resources, replicas, security context, `metrics.*` including the opt-in `metrics.serviceMonitor`, and the opt-in `grafana.dashboards`) |
| `README.md` | Chart-specific README rendered on Artifact Hub |

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `crds/` | CRD copy installed by Helm pre-install (see `crds/AGENTS.md`) |
| `dashboards/` | Copy of `config/grafana/dashboards/*.json`, read by the dashboards templates. Never edit it by hand: run `make helm-dashboards` (chart CI fails on chart PRs if it is stale) |
| `templates/` | Deployment, RBAC, ServiceAccount, NOTES, helpers (see `templates/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- The chart is released in its own PR after the operator tag build (merging a `config/helm-chart/` change publishes it, see `.github/workflows/chart-release.yaml`): `make helm-crds` and `make helm-dashboards`, `image.tag` and `appVersion` to the new version, bump the chart `version`.
- Helm does not upgrade CRDs in `crds/` after first install — breaking CRD changes require manual user action; document in chart `README.md` when bumping.
- Chart is tested in CI via `chart-testing` (`ct lint`, `ct install`) — see `.github/workflows/chart-test.yaml` and `ct-config.yaml`.
- When changing user-facing flags or RBAC in `config/manager/` or `config/rbac/`, mirror them into `templates/` so kustomize and Helm installs stay equivalent.

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
