# Grafana dashboards

The operator ships two Grafana dashboards built on its [metrics](metrics.md). Both need a Prometheus that scrapes
the operator with the `namespace` label of the Migration series kept (see
[Labels as scraped](metrics.md#labels-as-scraped)).

| Dashboard | uid | For | Shows |
|-----------|-----|-----|-------|
| Flyway operator | `flyway-operator` | The team running the operator (`alert_audience: platform`) | Scrape targets up, reconcile rate, errors and error ratio, reconcile latency, work queue depth, Migrations by condition and paused. |
| Flyway migrations | `flyway-migrations` | Teams owning Migrations (`alert_audience: workload`) | Per-Migration status, runs by result, last run duration, how long Migrations have been progressing, paused Migrations. |

The JSON files under [`config/grafana/dashboards/`](../config/grafana/dashboards) are the source of truth. Every
query uses a `datasource` variable of type Prometheus, so no datasource UID is hard-coded: pick the Prometheus
datasource from the dropdown at the top of the dashboard.

## Flyway operator

- **Scrape targets up**: operator targets with `up == 1`, matched by the `job` variable (default
  `.*flyway-operator.*`). `0` is what `FlywayOperatorMetricsAbsent` fires on; change the variable if your scrape
  job is named differently.
- **Reconcile error ratio (15m)**: the `FlywayOperatorReconcileErrors` expression, with its 10% threshold.
- **Reconcile rate** by result, with reconcile errors, and **reconcile latency** (p50, p90, p99), from
  `controller_runtime_reconcile_*{controller="migration"}`.
- **Work queue depth**: `workqueue_depth{controller="migration"}`.
- **Migrations by condition**: how many Migrations have `Applied`, `Progressing` or `Failed` set to True, and how
  many are paused.

## Flyway migrations

Variables: `datasource`, `namespace` (multi-select) and `migration` (multi-select, limited to the selected
namespaces).

- **Counters** at the top mirror the workload alerts: Failed (`FlywayMigrationFailed`), Stuck over 1h
  (`FlywayMigrationStuck`), Retrying in the last 30m (`FlywayMigrationRetrying`) and Slow over 15m
  (`FlywayMigrationSlow`), plus the number of Migrations and of paused ones. They show the alert expression now;
  an alert with a `for:` duration fires only after the expression has held that long.
- **Migration status**: one row per Migration with its `Applied`, `Progressing` and `Failed` conditions (True,
  False, or Unknown when not yet set), whether it is paused, the time since its most recent condition change, and
  the duration of its last successful run.
- **Runs by result**: finished Job attempts per interval, by `result` (`succeeded`, `retrying`, `failed`).
- **Last run duration**: per Migration and result. It is held in the operator's memory, so it is missing after an
  operator restart until the next run finishes.
- **Progressing for**: time since `Progressing` turned True, for Migrations progressing now. `FlywayMigrationStuck`
  fires above 1h.
- **Paused Migrations**: Migrations with the `flyway-operator.davidkarlsen.com/paused` annotation set to `true`.

## The ConfigMap

The kustomize install (`config/default`) and the OLM bundle create a ConfigMap `flyway-operator-dashboards` in the
operator's namespace (`flyway-operator-system` for kustomize, the install namespace for OLM). It has one key per
dashboard, `flyway-operator.json` and `flyway-migrations.json`, and the label `grafana_dashboard: "1"`.

The Helm chart does not ship the dashboards yet.

## grafana-operator v5

Create a `GrafanaDashboard` per dashboard. `spec.configMapRef` has no namespace field, so the `GrafanaDashboard`
must be in the same namespace as the ConfigMap, the operator's namespace. Replace `flyway-operator-system` if you
installed elsewhere, and set `instanceSelector` to match the labels of your `Grafana` resource:

```yaml
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: flyway-operator
  namespace: flyway-operator-system
spec:
  instanceSelector:
    matchLabels:
      dashboards: grafana # match the labels of your Grafana resource
  allowCrossNamespaceImport: true # Grafana runs in another namespace
  folder: Flyway # optional
  configMapRef:
    name: flyway-operator-dashboards
    key: flyway-operator.json
---
apiVersion: grafana.integreatly.org/v1beta1
kind: GrafanaDashboard
metadata:
  name: flyway-migrations
  namespace: flyway-operator-system
spec:
  instanceSelector:
    matchLabels:
      dashboards: grafana # match the labels of your Grafana resource
  allowCrossNamespaceImport: true # Grafana runs in another namespace
  folder: Flyway # optional
  configMapRef:
    name: flyway-operator-dashboards
    key: flyway-migrations.json
```

grafana-operator only imports a `GrafanaDashboard` into `Grafana` instances in the same namespace, unless
`allowCrossNamespaceImport` is `true`. Grafana usually runs in its own namespace, so keep it `true` there, and
remove it if your `Grafana` resource is in the operator's namespace. grafana-operator must also watch the
operator's namespace (its `WATCH_NAMESPACE` or cluster-wide mode).

The dashboard's `datasource` variable lists the Prometheus datasources of that Grafana and defaults to its default
one.

## Grafana sidecar

The Grafana Helm chart and kube-prometheus-stack can run a sidecar that loads dashboards from ConfigMaps with a
label. Its defaults are the label `grafana_dashboard` with value `"1"`, so the ConfigMap is picked up when the
sidecar watches the operator's namespace:

```yaml
# values of the grafana chart; under grafana: for kube-prometheus-stack
sidecar:
  dashboards:
    enabled: true
    label: grafana_dashboard
    labelValue: "1"
    searchNamespace: ALL # or a list that includes the operator's namespace
```

## Manual import

In Grafana, open **Dashboards > New > Import**, upload
[`flyway-operator.json`](../config/grafana/dashboards/flyway-operator.json) or
[`flyway-migrations.json`](../config/grafana/dashboards/flyway-migrations.json), and pick the Prometheus datasource
in the dashboard's `datasource` dropdown. To read them from a cluster:

```shell
kubectl -n flyway-operator-system get configmap flyway-operator-dashboards \
  -o jsonpath='{.data.flyway-migrations\.json}' > flyway-migrations.json
```

The dashboards use schema version 39, so they load in Grafana 11.0 and later; newer Grafana versions upgrade the
schema on load.
