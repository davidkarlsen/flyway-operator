# Metrics

The operator serves Prometheus metrics at `/metrics` on port 8443 over HTTPS. This is on by default in every
install path.

## What is served

- HTTPS on `:8443`, path `/metrics`.
- Every request is checked by the controller-runtime authn/authz filter
  ([`filters.WithAuthenticationAndAuthorization`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/metrics/filters)).
  The bearer token is validated with a `TokenReview`, then a `SubjectAccessReview` checks that the caller may
  `get` the non-resource URL `/metrics`.
- The certificate is self-signed and generated at startup, unless `--metrics-cert-path` points at a certificate.

Manager flags (`cmd/main.go`):

| Flag | Default | Meaning |
|------|---------|---------|
| `--metrics-bind-address` | `0` | Address the metrics server binds to. `0` disables it. The manifests set `:8443`. |
| `--metrics-secure` | `true` | Serve over HTTPS with authn/authz. `false` serves plain HTTP with no auth. |
| `--metrics-cert-path` | `""` | Directory containing the metrics certificate. Empty means self-signed. |
| `--metrics-cert-name` | `tls.crt` | Certificate file name inside `--metrics-cert-path`. |
| `--metrics-cert-key` | `tls.key` | Key file name inside `--metrics-cert-path`. |

## Per install path

### kustomize (`make deploy`)

`config/default` sets `--metrics-bind-address=:8443`, and also deploys:

- the `flyway-operator-controller-manager-metrics-service` Service (port `https`, 8443);
- the metrics RBAC;
- a `ServiceMonitor` from `config/prometheus`.

The `ServiceMonitor` is always applied, so the cluster needs the Prometheus Operator CRDs
(`monitoring.coreos.com/v1`). Without them the apply fails with `no matches for kind "ServiceMonitor"`. On such
clusters, remove `- ../prometheus` from `config/default/kustomization.yaml` first.

### OLM

The bundle is generated from `config/`. It ships the same Deployment args, the metrics Service, the metrics
RBAC and the `ServiceMonitor`. An OLM install on a cluster without the `ServiceMonitor` CRD has not been
tested.

### Helm

| Value | Default | Meaning |
|-------|---------|---------|
| `metrics.enabled` | `true` | Serve metrics, and render the `<fullname>-metrics` Service, the metrics-auth ClusterRole and binding, and the `<fullname>-metrics-reader` ClusterRole. `false` passes `--metrics-bind-address=0`. |
| `metrics.port` | `8443` | Metrics port on the container and the Service. |
| `metrics.serviceMonitor.enabled` | `false` | Render a `ServiceMonitor`. This is opt-in and needs the Prometheus Operator CRDs. |
| `metrics.serviceMonitor.interval` | `""` | Scrape interval. Empty uses the Prometheus default. |
| `metrics.serviceMonitor.labels` | `{}` | Extra labels on the `ServiceMonitor`, for example to match a Prometheus `serviceMonitorSelector`. |

The secure metrics server needs operator image 0.3.0 or newer. The chart defaults to 0.4.1. With an older
`image.tag`, metrics are served as plain HTTP without auth.

## Granting Prometheus access

Prometheus needs its ServiceAccount bound to the operator's `metrics-reader` ClusterRole. Without this binding,
scrapes return 403.

- kustomize and OLM: the ClusterRole is `flyway-operator-metrics-reader`.

  ```shell
  kubectl create clusterrolebinding flyway-operator-metrics-reader \
    --clusterrole=flyway-operator-metrics-reader \
    --serviceaccount=<prometheus-namespace>:<prometheus-serviceaccount>
  ```

- Helm: the ClusterRole is `<release-fullname>-metrics-reader`. `helm get notes <release>` prints the exact
  command, using a binding name unique to the release.

## Prometheus without the Prometheus Operator

This is a plain `scrape_config` for a Prometheus running in-cluster under a ServiceAccount that is bound as
described above. Replace `<metrics-service>` with the metrics Service name:
`flyway-operator-controller-manager-metrics-service` (kustomize or OLM) or `<release-fullname>-metrics` (Helm).

```yaml
scrape_configs:
  - job_name: flyway-operator
    scheme: https
    honor_labels: true # keep the Migration's namespace label, see "Labels as scraped"
    tls_config:
      insecure_skip_verify: true # self-signed certificate
    authorization:
      credentials_file: /var/run/secrets/kubernetes.io/serviceaccount/token
    kubernetes_sd_configs:
      - role: endpoints
    relabel_configs:
      - source_labels: [__meta_kubernetes_service_name]
        regex: <metrics-service>
        action: keep
      - source_labels: [__meta_kubernetes_endpoint_port_name]
        regex: https
        action: keep
```

## Verifying with curl

From a pod in the cluster, replacing `<namespace>` and `<metrics-service>`:

```shell
URL=https://<metrics-service>.<namespace>.svc:8443/metrics
curl -sk -o /dev/null -w '%{http_code}\n' "$URL"                                   # 401
curl -sk -H "Authorization: Bearer $TOKEN" "$URL" | grep controller_runtime_reconcile_total
```

Create `$TOKEN` with `kubectl create token <serviceaccount> -n <namespace>`.

| Response | Cause |
|----------|-------|
| `401` | No bearer token, or the `TokenReview` rejected it (expired or wrong audience). |
| `403` | The token is valid, but its ServiceAccount is not bound to the `metrics-reader` ClusterRole. |
| `200` | The ServiceAccount is authenticated and authorized. |
| `400` or a TLS error | Plain `http://` was used against the HTTPS port. A TLS error with `https://` means the server speaks plain HTTP with no auth: either `--metrics-secure=false` is set, or the image is older than 0.3.0. |
| Connection refused | kustomize or OLM with `--metrics-bind-address=0`: the Service exists, but nothing listens on 8443. |
| `Could not resolve host` | Helm with `metrics.enabled=false`: the `<release-fullname>-metrics` Service is not rendered. |

## Available metrics

### Migration metrics

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `flyway_operator_migration_status_condition` | gauge | `namespace`, `migration`, `condition`, `status` | One series per `status` (`true`, `false`, `unknown`) of each condition (`Applied`, `Progressing`, `Failed`); `1` for the current status, `0` for the others. A condition not yet set is `unknown`. |
| `flyway_operator_migration_status_condition_last_transition_timestamp_seconds` | gauge | `namespace`, `migration`, `condition` | Unix time of the condition's last status change. |
| `flyway_operator_migration_paused` | gauge | `namespace`, `migration` | `1` when the Migration is paused, else `0`. |
| `flyway_operator_migration_runs_total` | counter | `namespace`, `migration`, `result` | Finished Job attempts. `result` is `succeeded`, `retrying` (the attempt failed and another follows) or `failed` (the attempt failed and `retryLimit` is exhausted). Failed attempts are `retrying` + `failed`. |
| `flyway_operator_migration_last_run_duration_seconds` | gauge | `namespace`, `migration`, `result` | Run time of the last finished attempt with this result, from the Job's `status.startTime` to its `status.completionTime`, or to the time of its `Failed` condition. |

- The state gauges (`status_condition`, `last_transition_timestamp_seconds`, `paused`) are read from the
  operator's cache of Migrations at scrape time, and are served only by the elected leader. They match the
  persisted status, are back right after a restart, and disappear with the Migration.
- `runs_total` and `last_run_duration_seconds` are recorded by the reconciler, on the leader. The three
  `runs_total` series of a Migration start at `0` when the operator first reconciles it, so `increase()` also
  sees the first outcome. Both are removed when the Migration is deleted.
- An attempt is counted once: when a reconcile moves the Migration's status to that outcome for the current
  generation and attempt, and the status write succeeds. Further reconciles of the same finished Job, and
  reconciles after an operator restart, do not count it again.
- An attempt can go uncounted, never counted twice. This happens when the status write fails, or for a retried
  attempt, when deleting its Job fails: the status already records `Retrying` for that attempt, so the
  reconcile that then deletes the Job does not count it. A retried attempt whose status write failed after its
  Job was deleted is counted once if the Job is still terminating at the next reconcile, and stays uncounted if
  the Job is already gone. Attempts of a superseded generation are not counted.
- Counters start at zero when the operator restarts. Alert on the state gauges, and use `increase()` or
  `rate()` on counters. An attempt that fails while the operator is down is counted before the first scrape
  after the restart, so `increase()` misses it.
- `last_run_duration_seconds` is held in memory: after an operator restart or failover it is missing until the
  next run of that Migration finishes, and `FlywayMigrationSlow` resolves meanwhile.
- When the operator cannot list Migrations from its cache during a scrape, it logs the error and serves no
  state series for that scrape, so open alerts on them can resolve and fire again.

See [Alerting](#alerting) for rules built on these metrics.

### Standard metrics

The operator also serves the standard controller-runtime and Go client metrics, for example:

- **Reconciles:** `controller_runtime_reconcile_total`, `controller_runtime_reconcile_errors_total`,
  `controller_runtime_terminal_reconcile_errors_total`, `controller_runtime_reconcile_panics_total`,
  `controller_runtime_active_workers`, `controller_runtime_max_concurrent_reconciles`. The `controller` label
  is `migration`.
- **Work queue:** `workqueue_adds_total`, `workqueue_depth`, `workqueue_queue_duration_seconds`,
  `workqueue_work_duration_seconds`, `workqueue_retries_total`, `workqueue_unfinished_work_seconds`,
  `workqueue_longest_running_processor_seconds`.
- **Other:** `leader_election_master_status`, `rest_client_requests_total`, `certwatcher_*`.
- **Runtime:** `go_*` and `process_*`.

Some series, such as reconcile duration histograms, only appear after the first reconcile. List what your
install serves with:

```shell
curl -sk -H "Authorization: Bearer $TOKEN" "$URL" | grep '^# TYPE'
```

## Alerting

The rules come in two groups with different audiences, set in the `alert_audience` label:

| Group | `alert_audience` | Alerts | Goes to |
|-------|------------------|--------|---------|
| `flyway-operator.operator` | `platform` | `FlywayOperatorMetricsAbsent` (no operator scrape target is up), `FlywayOperatorReconcileErrors` (over 10% of Migration reconciles fail) | The team running the operator. |
| `flyway-operator.migrations` | `workload` | `FlywayMigrationFailed` (`Failed=True`: retries exhausted, or image volumes unsupported), `FlywayMigrationStuck` (`Progressing` for over 1h), `FlywayMigrationRetrying` (an attempt failed in the last 30m), `FlywayMigrationSlow` (the last successful run took over 15m; an example to tune) | The team owning the Migration, by its `namespace` label. |

- Migration alerts aggregate with `max by (namespace, migration)` or `sum by (namespace, migration)`, so they
  keep those two labels and are not reset when the operator pod changes during a rollout or failover.
- `FlywayMigrationStuck` measures time since `Progressing` turned true from its last-transition timestamp, so an
  operator restart does not reset it. `Progressing` stays true across retries, and every Job attempt is bounded by
  `spec.activeDeadline` (default `1h`), so a run ends on its own within `activeDeadline × (retryLimit + 1)`. Keep the
  threshold below that to hear about a hung run before it times out, or above it to hear only about Migrations that
  are still progressing after that.
- `FlywayMigrationFailed` covers both terminal reasons, `RetryLimitExceeded` and `TimedOut` (the last attempt
  exceeded `spec.activeDeadline`): it matches `Failed=True`, and `status_condition` has no `reason` label. A timed-out
  final attempt counts once in `flyway_operator_migration_runs_total{result="failed"}`. Read the reason with
  `kubectl describe migration` or from the `TimedOut` Warning event.
- `FlywayMigrationFailed` also covers `ImageVolumeUnsupported`: the API server dropped the migration source image
  volume from the Job (Kubernetes older than 1.35, or the `ImageVolume` feature gate off), and the operator deleted the
  Job. No Job attempt ran, so `runs_total` does not count it; it shows only in `status_condition` and the
  `ImageVolumeUnsupported` Warning event.
- Default severities are `warning` and `info`. Raise `FlywayMigrationFailed` to `critical` where a failed
  migration blocks a release, as in the per-namespace example below.

### Labels as scraped

The `namespace` label of the `flyway_operator_migration_*` series is the Migration's namespace. Prometheus
attaches its own `namespace` label to targets discovered through a `ServiceMonitor` (and to custom scrape configs
that map it), and without honoring the series' labels it renames theirs to `exported_namespace`. The shipped
`ServiceMonitor` sets `honorLabels: true` in every install path; for the Helm chart, from the chart version that
ships operator 0.5.0. Your own `ServiceMonitor` or scrape config needs `honorLabels: true` or
`honor_labels: true`, as in the example [above](#prometheus-without-the-prometheus-operator).

`FlywayOperatorMetricsAbsent` matches the scrape target's `job` label, which depends on how it is scraped:
the Service name with a `ServiceMonitor` (`flyway-operator-controller-manager-metrics-service` for kustomize
and OLM, `<release-fullname>-metrics` for Helm), or `job_name` with a plain scrape config. Adjust
`job=~".*flyway-operator.*"` if your names differ.

### Cluster-wide rules

With the Prometheus Operator, set `metadata.namespace` and `metadata.labels` so your Prometheus
`ruleNamespaceSelector` and `ruleSelector` pick the rule up:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: flyway-operator
  namespace: <prometheus-rules-namespace>
  labels: {} # match your Prometheus ruleSelector, e.g. release: kube-prometheus-stack
spec:
  groups:
    - name: flyway-operator.operator
      rules:
        - alert: FlywayOperatorMetricsAbsent
          expr: absent(up{job=~".*flyway-operator.*"} == 1)
          for: 10m
          labels:
            severity: warning
            alert_audience: platform
          annotations:
            summary: No flyway-operator metrics target is up, so no migration alert can fire.
        - alert: FlywayOperatorReconcileErrors
          expr: |
            sum(rate(controller_runtime_reconcile_errors_total{controller="migration"}[15m]))
              / sum(rate(controller_runtime_reconcile_total{controller="migration"}[15m])) > 0.1
          for: 15m
          labels:
            severity: warning
            alert_audience: platform
          annotations:
            summary: More than 10% of Migration reconciles failed over 15m. Check the operator logs.
    - name: flyway-operator.migrations
      rules:
        - alert: FlywayMigrationFailed
          expr: |
            max by (namespace, migration) (
              flyway_operator_migration_status_condition{condition="Failed", status="true"}
            ) == 1
          for: 1m
          labels:
            severity: warning
            alert_audience: workload
          annotations:
            summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} failed and is not retried.
            description: >-
              retryLimit is exhausted (condition reason TimedOut when the last attempt exceeded spec.activeDeadline, else RetryLimitExceeded), or the cluster does not support image volumes (ImageVolumeUnsupported). Check `kubectl -n {{ $labels.namespace }} describe migration {{ $labels.migration }}`
              and `kubectl -n {{ $labels.namespace }} logs job/{{ $labels.migration }} -c flyway`.
              A spec change starts a new run.
        - alert: FlywayMigrationStuck
          expr: |
            max by (namespace, migration) (
              flyway_operator_migration_status_condition{condition="Progressing", status="true"}
            ) == 1
            and on (namespace, migration)
            time() - max by (namespace, migration) (
              flyway_operator_migration_status_condition_last_transition_timestamp_seconds{condition="Progressing"}
            ) > 3600
            unless on (namespace, migration) max by (namespace, migration) (flyway_operator_migration_paused) == 1
          for: 5m
          labels:
            severity: warning
            alert_audience: workload
          annotations:
            summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} has been progressing for more than 1h.
            description: >-
              The Job may be waiting on a database lock, an unreachable database or an unschedulable pod.
              Check `kubectl -n {{ $labels.namespace }} describe migration {{ $labels.migration }}` and the Job's pods.
        - alert: FlywayMigrationRetrying
          expr: |
            sum by (namespace, migration) (
              increase(flyway_operator_migration_runs_total{result="retrying"}[30m])
            ) > 0
          labels:
            severity: info
            alert_audience: workload
          annotations:
            summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} had a failed attempt in the last 30m and is retrying.
        - alert: FlywayMigrationSlow
          expr: |
            max by (namespace, migration) (
              flyway_operator_migration_last_run_duration_seconds{result="succeeded"}
            ) > 900
          labels:
            severity: info
            alert_audience: workload
          annotations:
            summary: The last successful run of migration {{ $labels.namespace }}/{{ $labels.migration }} took {{ $value | humanizeDuration }}.
```

The same rules as a plain Prometheus rules file, for `rule_files`:

```yaml
groups:
  - name: flyway-operator.operator
    rules:
      - alert: FlywayOperatorMetricsAbsent
        expr: absent(up{job=~".*flyway-operator.*"} == 1)
        for: 10m
        labels:
          severity: warning
          alert_audience: platform
        annotations:
          summary: No flyway-operator metrics target is up, so no migration alert can fire.
      - alert: FlywayOperatorReconcileErrors
        expr: |
          sum(rate(controller_runtime_reconcile_errors_total{controller="migration"}[15m]))
            / sum(rate(controller_runtime_reconcile_total{controller="migration"}[15m])) > 0.1
        for: 15m
        labels:
          severity: warning
          alert_audience: platform
        annotations:
          summary: More than 10% of Migration reconciles failed over 15m. Check the operator logs.
  - name: flyway-operator.migrations
    rules:
      - alert: FlywayMigrationFailed
        expr: |
          max by (namespace, migration) (
            flyway_operator_migration_status_condition{condition="Failed", status="true"}
          ) == 1
        for: 1m
        labels:
          severity: warning
          alert_audience: workload
        annotations:
          summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} failed and is not retried.
          description: >-
            retryLimit is exhausted (condition reason TimedOut when the last attempt exceeded spec.activeDeadline, else RetryLimitExceeded), or the cluster does not support image volumes (ImageVolumeUnsupported). Check `kubectl -n {{ $labels.namespace }} describe migration {{ $labels.migration }}`
            and `kubectl -n {{ $labels.namespace }} logs job/{{ $labels.migration }} -c flyway`.
            A spec change starts a new run.
      - alert: FlywayMigrationStuck
        expr: |
          max by (namespace, migration) (
            flyway_operator_migration_status_condition{condition="Progressing", status="true"}
          ) == 1
          and on (namespace, migration)
          time() - max by (namespace, migration) (
            flyway_operator_migration_status_condition_last_transition_timestamp_seconds{condition="Progressing"}
          ) > 3600
          unless on (namespace, migration) max by (namespace, migration) (flyway_operator_migration_paused) == 1
        for: 5m
        labels:
          severity: warning
          alert_audience: workload
        annotations:
          summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} has been progressing for more than 1h.
          description: >-
            The Job may be waiting on a database lock, an unreachable database or an unschedulable pod.
            Check `kubectl -n {{ $labels.namespace }} describe migration {{ $labels.migration }}` and the Job's pods.
      - alert: FlywayMigrationRetrying
        expr: |
          sum by (namespace, migration) (
            increase(flyway_operator_migration_runs_total{result="retrying"}[30m])
          ) > 0
        labels:
          severity: info
          alert_audience: workload
        annotations:
          summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} had a failed attempt in the last 30m and is retrying.
      - alert: FlywayMigrationSlow
        expr: |
          max by (namespace, migration) (
            flyway_operator_migration_last_run_duration_seconds{result="succeeded"}
          ) > 900
        labels:
          severity: info
          alert_audience: workload
        annotations:
          summary: The last successful run of migration {{ $labels.namespace }}/{{ $labels.migration }} took {{ $value | humanizeDuration }}.
```

### Per-namespace rules

A team can instead deploy the migration rules in its own namespace, with every expression filtered to that
namespace. Replace `team-a`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: flyway-migrations
  namespace: team-a
spec:
  groups:
    - name: flyway-migrations.team-a
      rules:
        - alert: FlywayMigrationFailed
          expr: |
            max by (namespace, migration) (
              flyway_operator_migration_status_condition{namespace="team-a", condition="Failed", status="true"}
            ) == 1
          for: 1m
          labels:
            severity: critical  # raised from the default warning: a failed migration blocks this team's releases
            alert_audience: workload
          annotations:
            summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} failed and is not retried.
            description: >-
              retryLimit is exhausted (condition reason TimedOut when the last attempt exceeded spec.activeDeadline, else RetryLimitExceeded), or the cluster does not support image volumes (ImageVolumeUnsupported). Check `kubectl -n {{ $labels.namespace }} describe migration {{ $labels.migration }}`
              and `kubectl -n {{ $labels.namespace }} logs job/{{ $labels.migration }} -c flyway`.
              A spec change starts a new run.
        - alert: FlywayMigrationStuck
          expr: |
            max by (namespace, migration) (
              flyway_operator_migration_status_condition{namespace="team-a", condition="Progressing", status="true"}
            ) == 1
            and on (namespace, migration)
            time() - max by (namespace, migration) (
              flyway_operator_migration_status_condition_last_transition_timestamp_seconds{namespace="team-a", condition="Progressing"}
            ) > 3600
            unless on (namespace, migration) max by (namespace, migration) (flyway_operator_migration_paused{namespace="team-a"}) == 1
          for: 5m
          labels:
            severity: warning
            alert_audience: workload
          annotations:
            summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} has been progressing for more than 1h.
            description: >-
              The Job may be waiting on a database lock, an unreachable database or an unschedulable pod.
              Check `kubectl -n {{ $labels.namespace }} describe migration {{ $labels.migration }}` and the Job's pods.
        - alert: FlywayMigrationRetrying
          expr: |
            sum by (namespace, migration) (
              increase(flyway_operator_migration_runs_total{namespace="team-a", result="retrying"}[30m])
            ) > 0
          labels:
            severity: info
            alert_audience: workload
          annotations:
            summary: Migration {{ $labels.namespace }}/{{ $labels.migration }} had a failed attempt in the last 30m and is retrying.
        - alert: FlywayMigrationSlow
          expr: |
            max by (namespace, migration) (
              flyway_operator_migration_last_run_duration_seconds{namespace="team-a", result="succeeded"}
            ) > 900
          labels:
            severity: info
            alert_audience: workload
          annotations:
            summary: The last successful run of migration {{ $labels.namespace }}/{{ $labels.migration }} took {{ $value | humanizeDuration }}.
```

Where namespaced rules are evaluated with the rule's namespace enforced as a label filter (for example OpenShift
user-workload monitoring, or Prometheus Operator with `enforcedNamespaceLabel`), the series match only when they
carry the Migration's namespace in `namespace`, as described in [Labels as scraped](#labels-as-scraped). A
Prometheus that enforces the `namespace` label on the operator's own `ServiceMonitor` overrides `honorLabels`.

### Alertmanager routing

Route operator alerts to the platform team, and migration alerts by namespace to the owning team:

```yaml
route:
  receiver: default
  routes:
    - matchers: ['alert_audience="platform"']
      receiver: platform
    - matchers: ['alert_audience="workload"']
      group_by: [alertname, namespace, migration]
      receiver: default # namespaces without a team route
      routes:
        - matchers: ['namespace="team-a"']
          receiver: team-a
        - matchers: ['namespace=~"team-b|team-b-test"']
          receiver: team-b
receivers:
  - name: default
  - name: platform
  - name: team-a
  - name: team-b
```

Check rules with `promtool check rules <file>` and the Alertmanager config with `amtool check-config <file>`.
