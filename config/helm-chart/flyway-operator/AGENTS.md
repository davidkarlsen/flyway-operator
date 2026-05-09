<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# flyway-operator

## Purpose
Helm chart for installing the operator outside of OLM. Mirrors the deployment surface in `config/manager/` and `config/rbac/` but expressed as Go templates.

## Key Files

| File | Description |
|------|-------------|
| `Chart.yaml` | Chart metadata (name, version, appVersion) |
| `Chart.lock` | Pinned dependency versions |
| `values.yaml` | User-facing knobs (image, resources, replicas, namespace, RBAC toggles) |
| `README.md` | Chart-specific README rendered on Artifact Hub |

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `crds/` | CRD copy installed by Helm pre-install (see `crds/AGENTS.md`) |
| `templates/` | Deployment, RBAC, ServiceAccount, NOTES, helpers (see `templates/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Helm does not upgrade CRDs in `crds/` after first install — breaking CRD changes require manual user action; document in chart `README.md` when bumping.
- Chart is tested in CI via `chart-testing` (`ct lint`, `ct install`) — see `.github/workflows/chart-test.yaml` and `ct-config.yaml`.
- When changing user-facing flags or RBAC in `config/manager/` or `config/rbac/`, mirror them into `templates/` so kustomize and Helm installs stay equivalent.

<!-- MANUAL: -->
