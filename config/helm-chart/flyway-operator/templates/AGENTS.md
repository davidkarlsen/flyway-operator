<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# templates

## Purpose
Helm templates for the operator workload and its RBAC. Equivalent to `config/manager/` + `config/rbac/` rendered through Go templating with `values.yaml` overrides.

## Key Files

| File | Description |
|------|-------------|
| `_helpers.tpl` | Common name/label helpers (`flyway-operator.fullname`, labels block, etc.) |
| `deployment.yaml` | Operator Deployment — image/resources/probes from values; metrics bind arg and `https` port when `metrics.enabled` |
| `service.yaml` | Health-probe Service (`http` → 8081) |
| `metrics-service.yaml` | `<fullname>-metrics` Service (`https` → metrics port), gated on `metrics.enabled` |
| `metrics-auth-rbac.yaml` | Metrics authn/authz ClusterRole + binding and the `metrics-reader` ClusterRole, gated on `metrics.enabled` |
| `servicemonitor.yaml` | Prometheus Operator `ServiceMonitor`, gated on `metrics.enabled` and `metrics.serviceMonitor.enabled` |
| `serviceaccount.yaml` | Manager ServiceAccount |
| `role.yaml` / `role_binding.yaml` | Reconciler RBAC |
| `migration_editor_role.yaml` / `migration_viewer_role.yaml` | User-facing aggregated roles |
| `NOTES.txt` | Post-install message rendered by `helm install` |

## For AI Agents

### Working In This Directory
- Always run `helm lint` (or `ct lint --charts config/helm-chart/flyway-operator`) after editing.
- Keep this in sync with `config/manager/` and `config/rbac/`: a flag added to one should appear in the other.

<!-- MANUAL: -->
