<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# templates

## Purpose
Helm templates for the operator workload and its RBAC. Equivalent to `config/manager/` + `config/rbac/` rendered through Go templating with `values.yaml` overrides.

## Key Files

| File | Description |
|------|-------------|
| `_helpers.tpl` | Common name/label helpers (`flyway-operator.fullname`, labels block, etc.) |
| `deployment.yaml` | Operator Deployment — image/resources/probes from values |
| `service.yaml` | Metrics service |
| `serviceaccount.yaml` | Manager ServiceAccount |
| `role.yaml` / `role_binding.yaml` | Reconciler RBAC |
| `migration_editor_role.yaml` / `migration_viewer_role.yaml` | User-facing aggregated roles |
| `NOTES.txt` | Post-install message rendered by `helm install` |

## For AI Agents

### Working In This Directory
- Always run `helm lint` (or `ct lint --charts config/helm-chart/flyway-operator`) after editing.
- Keep this in sync with `config/manager/` and `config/rbac/`: a flag added to one should appear in the other.

<!-- MANUAL: -->
