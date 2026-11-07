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

The operator does not register metrics of its own. It serves the standard controller-runtime and Go client
metrics, for example:

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
