# Metrics

The operator serves Prometheus metrics at `/metrics` on port 8443 over HTTPS, on by default in every install path.

## What is served

- Every request is checked by the controller-runtime authn/authz filter
  ([`filters.WithAuthenticationAndAuthorization`](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/metrics/filters)):
  a `TokenReview` validates the bearer token, then a `SubjectAccessReview` checks that the caller may `get` the
  non-resource URL `/metrics`.
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
- a `ServiceMonitor` and the alert `PrometheusRule` from `config/prometheus`, see [Shipped rules](#shipped-rules);
- the `flyway-operator-dashboards` ConfigMap from `config/grafana`, see [Grafana dashboards](dashboards.md).

The `ServiceMonitor` and `PrometheusRule` are always applied, so the cluster needs the Prometheus Operator CRDs
(`monitoring.coreos.com/v1`). Without them the apply fails with `no matches for kind "ServiceMonitor"` (or `"PrometheusRule"`).
On such clusters, remove `- ../prometheus` from `config/default/kustomization.yaml` first.

### OLM

The bundle is generated from `config/` and ships the same Deployment args, metrics Service, metrics RBAC,
`ServiceMonitor`, alert `PrometheusRule` and dashboards ConfigMap. An OLM install on a cluster without the
`ServiceMonitor` CRD has not been tested.

### Helm

| Value | Default | Meaning |
|-------|---------|---------|
| `metrics.enabled` | `true` | Serve metrics, and render the `<fullname>-metrics` Service, the metrics-auth ClusterRole and binding, and the `<fullname>-metrics-reader` ClusterRole. `false` passes `--metrics-bind-address=0`. |
| `metrics.port` | `8443` | Metrics port on the container and the Service. |
| `metrics.serviceMonitor.enabled` | `false` | Render a `ServiceMonitor`. Opt-in; needs the Prometheus Operator CRDs. |
| `metrics.serviceMonitor.interval` | `""` | Scrape interval. Empty uses the Prometheus default. |
| `metrics.serviceMonitor.labels` | `{}` | Extra labels on the `ServiceMonitor`, for example to match a Prometheus `serviceMonitorSelector`. |
| `prometheusRule.enabled` | `false` | Render the alert `PrometheusRule`, see [Shipped rules](#shipped-rules). Opt-in; needs the Prometheus Operator CRDs. |
| `prometheusRule.namespace` | `""` | Namespace of the `PrometheusRule`. Empty uses the release namespace. |
| `prometheusRule.additionalLabels` | `{}` | Extra labels on the `PrometheusRule`, for example to match a Prometheus `ruleSelector`. |

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

A plain `scrape_config` for a Prometheus running in-cluster under a ServiceAccount bound as described above. Replace
`<metrics-service>` with the metrics Service name (see [Labels as scraped](#labels-as-scraped)).

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
| `400` or a TLS error | Plain `http://` was used against the HTTPS port. A TLS error with `https://` means the server speaks plain HTTP with no auth: `--metrics-secure=false` is set. |
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

- The state gauges (`status_condition`, `last_transition_timestamp_seconds`, `paused`) are read from the operator's
  cache of Migrations at scrape time and served only by the elected leader. They match the persisted status, are back
  right after a restart, and disappear with the Migration.
- `runs_total` and `last_run_duration_seconds` are recorded by the reconciler, on the leader. The three `runs_total`
  series of a Migration start at `0` when the operator first reconciles it, so `increase()` also sees the first
  outcome. Both are removed when the Migration is deleted.
- An attempt is counted once: when a reconcile moves the Migration's status to that outcome for the current
  generation and attempt, and the status write succeeds. Further reconciles of the same finished Job, and reconciles
  after an operator restart, do not count it again.
- An attempt can go uncounted, never counted twice. This happens when the status write fails, or for a retried
  attempt, when deleting its Job fails: the status already records `Retrying` for that attempt, so the reconcile that
  then deletes the Job does not count it. A retried attempt whose status write failed after its Job was deleted is
  counted once if the Job is still terminating at the next reconcile, and stays uncounted if the Job is already gone.
  Attempts of a superseded generation are not counted.
- `runs_total` and `last_run_duration_seconds` are held in memory. Counters start at zero when the operator restarts
  or the leader changes, and the duration is missing until the next run of that Migration finishes. Alert on the state
  gauges, and use `increase()` or `rate()` on counters. An attempt that fails while the operator is down is counted
  before the first scrape after the restart, so `increase()` misses it.
- When the operator cannot list Migrations from its cache during a scrape, it logs the error and serves no state
  series for that scrape, so open alerts on them can resolve and fire again.
- A timed-out final attempt counts once in `runs_total{result="failed"}`. `ImageVolumeUnsupported` runs no Job, so
  `runs_total` does not count it; it shows only in `status_condition` and the `ImageVolumeUnsupported` Warning event.
  `status_condition` has no `reason` label: read the reason with `kubectl describe migration`.

[Alerting](#alerting) lists the shipped rules built on these metrics, and [Grafana dashboards](dashboards.md) mirror them.

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

The rules come in two groups with different audiences, set in the `alert_audience` label. Each alert's
`runbook_url` points at its section of the [alert runbook](runbook.md), which explains its meaning, impact,
diagnosis and mitigation.

| Group | `alert_audience` | Alert | Fires when | Goes to |
|-------|------------------|-------|------------|---------|
| `flyway-operator.operator` | `platform` | [`FlywayOperatorMetricsAbsent`](runbook.md#flywayoperatormetricsabsent) | no operator scrape target is up | The team running the operator. |
| | | [`FlywayOperatorReconcileErrors`](runbook.md#flywayoperatorreconcileerrors) | over 10% of Migration reconciles fail | |
| `flyway-operator.migrations` | `workload` | [`FlywayMigrationFailed`](runbook.md#flywaymigrationfailed) | `Failed=True`: retries exhausted, timed out, or image volumes unsupported | The team owning the Migration, by its `namespace` label. |
| | | [`FlywayMigrationStuck`](runbook.md#flywaymigrationstuck) | `Progressing` for over 1h | |

- Migration alerts aggregate with `max by (namespace, migration)`, so they keep those two labels and ignore which
  operator pod served the series. Only the elected leader serves the state series, so they can be briefly missing
  during a rollout or failover; `FlywayMigrationFailed` and `FlywayMigrationStuck` have `keep_firing_for: 5m` so a
  short gap does not resolve and re-fire them.
- All shipped alerts are `warning`. Raise `FlywayMigrationFailed` to `critical` where a failed migration blocks a
  release, by patching the `severity` as described in [Shipped rules](#shipped-rules).

### Labels as scraped

The `namespace` label of the `flyway_operator_migration_*` series is the Migration's namespace. Prometheus attaches its
own `namespace` label to targets discovered through a `ServiceMonitor` (and to custom scrape configs that map it), and
without honoring the series' labels it renames theirs to `exported_namespace`. The shipped `ServiceMonitor` sets
`honorLabels: true` in every install path; for the Helm chart, from the chart version that ships operator 0.5.0. Your
own `ServiceMonitor` or scrape config needs `honorLabels: true` or `honor_labels: true`, as in the example
[above](#prometheus-without-the-prometheus-operator).

The metrics Service is `flyway-operator-controller-manager-metrics-service` (kustomize and OLM) or
`<release-fullname>-metrics` (Helm). `FlywayOperatorMetricsAbsent` matches the scrape target's `job` label: the Service
name with a `ServiceMonitor`, or `job_name` with a plain scrape config. Adjust `job=~".*flyway-operator.*"` if your
names differ.

### Shipped rules

The rules are a `PrometheusRule` in `config/prometheus/rules.yaml`, and the same groups in the Helm chart
(`files/rules.yaml`). Those files hold the alerts and their thresholds.

| Install path | Rules | Disable or override |
|--------------|-------|---------------------|
| kustomize (`make deploy`) | On by default, as `controller-manager-alerts` in the operator namespace. | Remove `rules.yaml` from `config/prometheus/kustomization.yaml`, or patch the `PrometheusRule` in your overlay (for example to raise a `severity` or change a `for:`). |
| OLM | On by default, shipped in the bundle. | Delete the `PrometheusRule` after install, or add your own rules with different thresholds. A deletion or edit is undone by the next upgrade. |
| Helm | Off by default. | `prometheusRule.enabled=true` renders the `PrometheusRule`; `prometheusRule.namespace` sets its namespace (default: the release namespace) and `prometheusRule.additionalLabels` adds labels, for example `release: kube-prometheus-stack` for a Prometheus `ruleSelector`. Without the `monitoring.coreos.com/v1` `PrometheusRule` API nothing is rendered and the install notes warn; for offline rendering pass `--api-versions monitoring.coreos.com/v1/PrometheusRule` to `helm template`. |

Like the `ServiceMonitor`, the kustomize and OLM rule needs the Prometheus Operator CRDs. To use a different
threshold or a per-namespace scope, copy the groups from `config/prometheus/rules.yaml`, add a `namespace`
matcher to the selectors, and disable the shipped rule. With a Prometheus that enforces the rule namespace as a
`namespace` label (for example OpenShift user-workload monitoring, or Prometheus Operator with
`enforcedNamespaceLabel`), the series match only when they carry the Migration's namespace in `namespace`, as
described in [Labels as scraped](#labels-as-scraped). A Prometheus that enforces the `namespace` label on the
operator's own `ServiceMonitor` overrides `honorLabels`.

On OpenShift, the rules need the operator namespace to be monitored by the platform Prometheus: the label
`openshift.io/cluster-monitoring=true` and a `prometheus-k8s` Role and RoleBinding for service discovery. Check them
with `oc get ns <ns> --show-labels` and `oc get rolebinding prometheus-k8s -n <ns>`. The platform Prometheus needs
no metrics-reader binding: its own `prometheus-k8s` ClusterRole grants `get` on `/metrics`. Under user-workload
monitoring the namespace is enforced, so the Migration alerts only see their own namespace.

For a Prometheus without the Prometheus Operator, use the `spec` of `config/prometheus/rules.yaml` (the `groups:`
list, for example from `yq '.spec'`) as a rules file in `rule_files`. To check the rules, extract the groups first,
then run promtool on the result:

```shell
yq '.spec' config/prometheus/rules.yaml > /tmp/rules.yaml
promtool check rules /tmp/rules.yaml
```

`make test-rules` does this and also runs the unit tests in `config/prometheus/tests/rules_test.yaml` (needs `yq`;
promtool runs through `go run`). CI runs it in the build job.

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

Check the Alertmanager config with `amtool check-config <file>`.
