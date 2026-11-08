# Alert runbook

One section per alert shipped in `config/prometheus/rules.yaml` (see [Shipped rules](metrics.md#shipped-rules)). Each
alert's `runbook_url` points at its section. `<ns>` is the Migration's namespace, `<name>` its name, and `<op-ns>`
the operator's namespace. The operator Deployment is `flyway-operator-controller-manager` (kustomize and OLM) or
`<release>-flyway-operator` (Helm). On OpenShift use `oc` for `kubectl`.

## FlywayOperatorMetricsAbsent

**Meaning.** `absent(up{job=~".*flyway-operator.*"} == 1)` has held for 10 minutes (`for: 10m`, no
`keep_firing_for`): Prometheus has no scrape target of the operator that is up. Severity `warning`, audience
`platform`.

**Impact.** No Migration alert can fire while this one is active, so a failed or stuck Migration goes unnoticed. The
operator itself may still be working.

**Diagnosis.**

```shell
kubectl -n <op-ns> get deploy,pods                       # is the operator running, and is a leader elected?
kubectl -n <op-ns> logs deploy/<operator-deployment>     # crash loop, leader election, metrics server errors
kubectl -n <op-ns> get svc,servicemonitor                # metrics Service and ServiceMonitor exist
```

In Prometheus, look at the Targets page for the `flyway-operator` target: `403` means the Prometheus ServiceAccount is
not bound to the metrics-reader ClusterRole, a TLS error or `400` means metrics are served as plain HTTP (see
[Verifying with curl](metrics.md#verifying-with-curl)), and no target at all means the `ServiceMonitor` is not picked
up. If the `job` label of your target does not contain `flyway-operator`, the alert expression does not match it.

**Mitigation.**
- Operator down: fix the cause shown in its logs or pod events, then check that it is `Ready`.
- `403`: bind the Prometheus ServiceAccount to the metrics-reader ClusterRole, see
  [Granting Prometheus access](metrics.md#granting-prometheus-access).
- No target: check that the `ServiceMonitor` labels match your Prometheus `serviceMonitorSelector`, and for Helm that
  `metrics.enabled` and `metrics.serviceMonitor.enabled` are `true`.
- OpenShift platform Prometheus: the operator namespace needs the label `openshift.io/cluster-monitoring=true` and a
  `prometheus-k8s` RoleBinding for service discovery (`oc get ns <op-ns> --show-labels`,
  `oc get rolebinding prometheus-k8s -n <op-ns>`). No metrics-reader binding is needed. Under user-workload monitoring
  the namespace is enforced and the rule only sees its own namespace.

## FlywayOperatorReconcileErrors

**Meaning.** More than 10% of Migration reconciles of one operator namespace failed over the last 15 minutes
(`controller_runtime_reconcile_errors_total / controller_runtime_reconcile_total`, `controller="migration"`,
`sum by (namespace)`), and has held for 15 minutes (`for: 15m`, no `keep_firing_for`). Severity `warning`, audience
`platform`.

**Impact.** The operator is failing to reconcile some or all Migrations: Jobs may not be created, replaced or
deleted, and the status may be stale. Running Jobs are not affected.

**Diagnosis.**

```shell
kubectl -n <op-ns> logs deploy/<operator-deployment> | grep -i error
kubectl get events -A --field-selector reason=ProcessingError     # which Migrations, and the error
kubectl -n <ns> describe migration <name>                # the ReconcileError condition message
```

Typical causes: the API server rejects the Job (admission, quota, Pod Security), the operator lacks RBAC (for
example `list` on pods or events, see [When the pod does not start](usage.md#when-the-pod-does-not-start)), or the API
server is unavailable.

**Mitigation.** Fix the cause named in the error. For a rejected Job, correct the Migration spec (a spec change starts
a new reconcile). For RBAC, check the operator's ClusterRole against the one in the release you installed; the Helm
chart's ClusterRole can lag the operator image, so upgrade the chart. The alert resolves when the failing share drops
under 10% for the 15-minute window.

## FlywayMigrationFailed

**Meaning.** The `Failed` condition of a Migration is `True` (`flyway_operator_migration_status_condition{condition="Failed",
status="true"}`) for 1 minute (`for: 1m`), and the alert keeps firing for 5 minutes after the series goes missing
(`keep_firing_for: 5m`, because only the elected leader serves it). `Failed=True` means one of:
- `RetryLimitExceeded`: the Job failed on its last attempt (`spec.retryLimit + 1` attempts);
- `TimedOut`: the last attempt exceeded `spec.activeDeadline`;
- `ImageVolumeUnsupported`: the API server dropped the migration source image volume from the Job (Kubernetes older
  than 1.35, or the `ImageVolume` feature gate off), so no Job ran.

Severity `warning`, audience `workload`. A paused Migration still raises it.

**Impact.** The database was not migrated to this generation of the Migration, and the operator stops retrying. The
application waiting for the schema (an Argo CD sync, a rollout) stays blocked. A failed Flyway run can leave a failed
row in the schema history table; on databases without transactional DDL it can leave a script partly applied.

**Diagnosis.**

```shell
kubectl get migration <name> -n <ns> -o wide
kubectl get migration <name> -n <ns> -o jsonpath='{.status.conditions[?(@.type=="Failed")]}'   # reason and message
kubectl -n <ns> describe migration <name>                # Warning events: Failed, TimedOut, ImageVolumeUnsupported
kubectl -n <ns> logs job/<name> -c flyway                # the Flyway error of the last attempt
kubectl -n <ns> describe job <name>
```

The message holds the Job's own failure reason, followed by `; flyway: ` and Flyway's own error text (the failing
script and the database's error) when the Flyway log was readable. That needs `get` on `pods/log`: the kustomize and
OLM installs grant it, Helm users get it with the next chart release, and without it the message has no Flyway error.
The text can quote values from the failing statement; see [Status](usage.md#status). The Job of the last attempt is
kept, so its logs are readable; the Jobs of earlier attempts were deleted for the retry.

`status.schemaVersion` (the `SCHEMA VERSION` column of `kubectl get migration`) is the Flyway schema version the last
succeeded Job reported. It does not move on a failed run, so it tells which version the database was at before the
failure; on a database without transactional DDL a partly applied script is not reflected in it.

| Reason | Look at |
| --- | --- |
| `RetryLimitExceeded` | the Flyway log: a failing SQL script, a checksum mismatch, a failed row in the schema history, wrong credentials or an unreachable database |
| `TimedOut` | whether the migration is slow, or waiting on a lock or an unreachable database; compare with `spec.activeDeadline` (default `1h`) |
| `ImageVolumeUnsupported` | `kubectl version` (needs 1.35 or later) and the `ImageVolume` feature gate; see [Installing](installing.md#requirements) |

If the Job never started a container, the cause shows in the `Progressing` message of earlier attempts and in
`kubectl describe pod -l batch.kubernetes.io/job-name=<name>` (image pull, scheduling, admission), see
[When the pod does not start](usage.md#when-the-pod-does-not-start).

**Mitigation.** A new run starts only when the spec changes, because the Job is tied to the Migration's generation.
- SQL error: fix the SQL, build a new source image and set `spec.migrationSource.imageRef` to it.
- Failed row in the schema history, or a partly applied script after a timeout: clean up the database by hand if
  needed, then run `repair` first by setting `spec.flywayConfiguration.commands: [repair, migrate, info]`. Put the
  default `[info, migrate, info]` back once it has run, which is another spec change and runs Flyway again.
- Timeout on a legitimately long migration: raise `spec.activeDeadline`. Set `lock_timeout` or
  `statement_timeout` in `jdbcProperties` for runs that wait on a lock.
- Transient failure with an unchanged spec: change something harmless such as a `jobMetadata` annotation, or delete
  and re-create the Migration, to start a new Job.
- `ImageVolumeUnsupported`: move to a cluster with Kubernetes 1.35 or later, or enable the `ImageVolume` gate, then
  change the spec to try again.
- Not for now: pause with `kubectl annotate migration <name> -n <ns> flyway-operator.davidkarlsen.com/paused=true`.
  Pausing stops reconciliation but keeps `Failed=True`, so the alert keeps firing; remove the annotation to resume.

## FlywayMigrationStuck

**Meaning.** The `Progressing` condition of a Migration has been `True` for more than 1 hour
(`flyway_operator_migration_status_condition_last_transition_timestamp_seconds`, `time() - ... > 3600`), and that has
held for 5 minutes (`for: 5m`); the alert keeps firing for 5 minutes after the series goes missing
(`keep_firing_for: 5m`). It does not fire for a paused Migration. Severity `warning`, audience `workload`.

`Progressing` stays `True` across retries, and each attempt is bounded by `spec.activeDeadline` (default `1h`), so a run
ends on its own within `activeDeadline x (retryLimit + 1)`. With the defaults this alert can fire before the Migration
times out.

**Impact.** The migration has not finished, so the schema is not at the expected version. A Job of an older generation
that is still running also blocks the new one (`PreviousJobRunning`), so later spec changes wait.

**Diagnosis.**

```shell
kubectl get migration <name> -n <ns> -o wide
kubectl get migration <name> -n <ns> -o jsonpath='{.status.conditions[?(@.type=="Progressing")]}'   # reason and message
kubectl -n <ns> get job,pods -l batch.kubernetes.io/job-name=<name>
kubectl -n <ns> logs job/<name> -c flyway                # is Flyway running a long script or waiting on a lock?
kubectl -n <ns> describe pod -l batch.kubernetes.io/job-name=<name>
```

Read the reason:
- `JobRunning` with a message after "is running;" (`pod not created`, `pod unschedulable`, `image pull failed`,
  `container cannot start`): the pod does not start, see
  [When the pod does not start](usage.md#when-the-pod-does-not-start).
- `JobRunning` without such a detail: Flyway is running. Check the log for a long script, a database lock held by
  another session, or an unreachable database.
- `Retrying`: an attempt failed and another follows; the message holds the failure and, when the log was readable,
  Flyway's error after `; flyway: ` (needs `pods/log`, see [FlywayMigrationFailed](#flywaymigrationfailed)).
- `PreviousJobRunning`: a Job of an older generation is still running and the new spec waits for it.

**Mitigation.**
- Pod does not start: fix the cause (quota, image, scheduling). For a Job that never started a container, correcting
  the Migration spec replaces it (`PreviousJobReplaced`).
- Waiting on a lock or the database: release the lock or restore connectivity. The attempt ends at
  `activeDeadline`; set `lock_timeout` or `statement_timeout` through `jdbcProperties` so it fails cleanly.
- A hung Job of an older generation (`PreviousJobRunning`), or one whose Flyway container started and hangs, for
  example behind an Istio sidecar: delete it with `kubectl delete job <name> -n <ns>` to start the new one at once.
  Deleting a Job whose Flyway container is mid-migration can leave a partly applied script on databases without
  transactional DDL; check the schema history table afterwards.
- A migration that legitimately runs longer than the alert threshold: this is expected; raise `spec.activeDeadline`
  to cover it, and silence or tune the alert for that Migration.
