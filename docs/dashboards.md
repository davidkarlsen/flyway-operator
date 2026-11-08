# Grafana dashboards

The operator ships two Grafana dashboards built on its [metrics](metrics.md). Both need a Prometheus that scrapes
the operator with the Migration series' `namespace` label kept ([Labels as scraped](metrics.md#labels-as-scraped)).

| Dashboard | uid | For |
|-----------|-----|-----|
| Flyway operator | `flyway-operator` | The team running the operator (`alert_audience: platform`) |
| Flyway migrations | `flyway-migrations` | Teams owning Migrations (`alert_audience: workload`) |

The JSON files under [`config/grafana/dashboards/`](../config/grafana/dashboards) are the source of truth. Every
query uses a Prometheus `datasource` variable, so no datasource UID is hard-coded: pick it from the dropdown at the top; it lists the Grafana's Prometheus datasources and defaults to its default one.

## Flyway operator

- **Scrape targets up**: operator targets with `up == 1`, matched by the `job` variable (default
  `.*flyway-operator.*`; change it if your scrape job is named differently). `0` is what `FlywayOperatorMetricsAbsent`
  fires on.
- **Reconcile error ratio (15m)**: the `FlywayOperatorReconcileErrors` expression with its 10% threshold. With no
  reconciles in the last 15m it shows "no reconciles".
- **Reconcile rate** by result, with reconcile errors, and **reconcile latency** (p50, p90, p99), from
  `controller_runtime_reconcile_*{controller="migration"}`.
- **Work queue depth**: `workqueue_depth{controller="migration"}`.
- **Migrations by condition**: how many Migrations have `Applied`, `Progressing` or `Failed` True, and how many are paused.

## Flyway migrations

Variables: `datasource`, `namespace` (multi-select) and `migration` (multi-select, limited to the selected
namespaces).

- **Counters** at the top: Failed (`FlywayMigrationFailed`) and Stuck over 1h (`FlywayMigrationStuck`) mirror the
  shipped workload alerts; Retrying in the last 30m and Slow over 15m have no shipped alert. Further counts are the
  number of Migrations and of paused ones. They show the alert expression now; an alert with a `for:` duration fires
  only after the expression has held that long.
- **Migration status**: one row per Migration with its `Applied`, `Progressing` and `Failed` conditions (True,
  False, or Unknown when not yet set), whether it is paused, the time since its most recent condition change, and
  the duration of its last successful run.
- **Runs by result**: finished Job attempts per interval, by `result` (`succeeded`, `retrying`, `failed`).
- **Last run duration**: per Migration and result.
- **Progressing for**: time since `Progressing` turned True, for Migrations progressing now. `FlywayMigrationStuck`
  fires above 1h.
- **Paused Migrations**: Migrations with the `flyway-operator.davidkarlsen.com/paused` annotation set to `true`.

Runs by result and Last run duration start over after an operator restart or leader change, see [Migration metrics](metrics.md#migration-metrics). The condition and paused panels are back right away.

## The ConfigMap

The kustomize install (`config/default`) and the OLM bundle create a ConfigMap `flyway-operator-dashboards` in the
operator's namespace (`flyway-operator-system` for kustomize, the install namespace for OLM). It has one key per
dashboard, `flyway-operator.json` and `flyway-migrations.json`, and the label `grafana_dashboard: "1"`.

The Helm chart ships them too, see [Helm](#helm).

The ConfigMap is about 70 KB. `make deploy` applies with client-side `kubectl apply`, which stores a copy of each object
in the `kubectl.kubernetes.io/last-applied-configuration` annotation, capped at 256 KiB in total with the object's other
annotations, so the dashboards fit with room to grow. Server-side apply (`kubectl apply --server-side`) and OLM do not
use that annotation.

Use one import path per Grafana: the grafana-operator, the sidecar, or a manual import. The dashboard uids are fixed,
so a second path imports the same uids and the two overwrite each other.

## Helm

The chart creates the same ConfigMap, named `<release fullname>-dashboards` (for example
`flyway-operator-dashboards`) in the release namespace, when you set `grafana.dashboards.enabled=true`. It is off
by default. The ConfigMap has the keys `flyway-operator.json` and `flyway-migrations.json` and the labels from
`grafana.dashboards.labels` (default `grafana_dashboard: "1"`), so the [Grafana sidecar](#grafana-sidecar) loads
it as is.

```yaml
grafana:
  dashboards:
    enabled: true
    grafanaDashboard:
      instanceSelector:
        matchLabels:
          dashboards: grafana # match the labels of your Grafana resource
      allowCrossNamespaceImport: true
      folder: Flyway # optional
```

When the cluster serves `grafana.integreatly.org/v1beta1` (grafana-operator v5), the chart also creates the two
`GrafanaDashboard`s of [grafana-operator v5](#grafana-operator-v5) in the release namespace, with
`configMapRef` pointing at the ConfigMap. If the API is not served they are skipped, not failed, and the install
notes say so; for `helm template` or GitOps rendering without a cluster pass
`--api-versions grafana.integreatly.org/v1beta1`. `instanceSelector` is immutable once the `GrafanaDashboard` exists, and so is
`allowCrossNamespaceImport` from `true` to `false`, see below.

Use one import path per Grafana. With the sidecar, set `grafana.dashboards.grafanaDashboard.enabled=false` so
only the ConfigMap is created. If you use grafana-operator in a cluster that also runs a sidecar watching the
release namespace, set `grafana.dashboards.labels: {}` so the sidecar does not import the ConfigMap a second time.
The dashboard uids are fixed, so two releases, or two clusters, feeding the same Grafana overwrite each other's
dashboards: install the dashboards once per Grafana. The chart's copy of the JSON is kept in sync with `config/grafana/dashboards/` by
`make helm-dashboards`.

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

Choose both fields before the first apply. `instanceSelector` is immutable, and changing `allowCrossNamespaceImport`
from `true` to `false` is rejected as well: to make either change, delete and recreate the `GrafanaDashboard`.
Changing it from `false` to `true` is allowed.

## Grafana sidecar

The Grafana Helm chart and kube-prometheus-stack can run a sidecar that loads dashboards from ConfigMaps with the
label `grafana_dashboard`. The defaults differ: kube-prometheus-stack matches the label value `"1"`, while the
upstream grafana chart sets no `labelValue` and matches any value. Either way the ConfigMap is picked up when the
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
[`flyway-migrations.json`](../config/grafana/dashboards/flyway-migrations.json). To read them from a cluster:

```shell
kubectl -n flyway-operator-system get configmap flyway-operator-dashboards \
  -o jsonpath='{.data.flyway-migrations\.json}' > flyway-migrations.json
```

The dashboards use schema version 39, so they load in Grafana 11.0 and later; newer Grafana versions upgrade the
schema on load.
