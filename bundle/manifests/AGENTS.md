<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# manifests

## Purpose
Generated bundle manifests installed by OLM when the operator is subscribed to.

## Key Files

| File | Description |
|------|-------------|
| `flyway-operator.clusterserviceversion.yaml` | Generated CSV; merged from `config/manifests/bases/` + install strategy from `config/default/` |
| `flyway.davidkarlsen.com_migrations.yaml` | Generated CRD copy |
| `flyway-operator-controller-manager-metrics-service_v1_service.yaml` | Metrics Service |
| `flyway-operator-controller-manager-metrics-monitor_monitoring.coreos.com_v1_servicemonitor.yaml` | ServiceMonitor for Prometheus Operator |
| `flyway-operator-metrics-reader_rbac.authorization.k8s.io_v1_clusterrole.yaml` | ClusterRole for reading metrics |

## For AI Agents

### Working In This Directory
- Do not hand-edit. Run `make bundle`.

<!-- MANUAL: -->
