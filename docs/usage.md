# Usage

After [installing](installing.md) the operator, create `Migration` resources in the namespace of your application.

## The migration source image

Put your Flyway SQL files (`V1__init.sql`, ...) in a container image, by default under `/sql`.
The image **must contain `cp`**, because an init container uses it to copy the files out.
A minimal image:

```dockerfile
FROM busybox
COPY sql/ /sql/
```

## The `Migration` resource

```yaml
apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: my-app
  namespace: my-app
spec:
  database:
    username: myuser                 # passed as FLYWAY_USER
    credentials:                     # Secret key passed as FLYWAY_PASSWORD
      name: my-app-db
      key: password
    jdbcUrl: "jdbc:postgresql://my-db:5432/my_app"  # must start with jdbc:
  retryLimit: 2                      # optional, default 2: re-creations of a failed Job
  jobMetadata:                       # optional, labels and annotations set on the Job
    labels:
      example.com/team: payments
    annotations: {}
  podMetadata:                       # optional, labels and annotations set on the Job's pod
    labels: {}
    annotations:
      sidecar.istio.io/inject: "false"
  migrationSource:
    imageRef: "registry.example.com/my-app-sql:1.4.0"
    path: /sql                       # optional, default /sql
    encoding: UTF-8                  # optional, default UTF-8
    ImagePullSecret:                 # optional
      - name: my-registry-pull
    placeholders:                    # optional, Flyway placeholders
      tablespace: USERSPACE1
  flywayConfiguration:               # all fields optional
    flywayImage: docker.io/flyway/flyway:13
    commands: [info, migrate, info]  # default
    defaultSchema: MYSCHEMA
    baselineOnMigrate: true
    jdbcProperties: {}               # passed as Flyway jdbcProperties
    envVars: []                      # extra env vars for the Flyway container
    volumes: []                      # extra volumes for the Job
    volumeMounts: []                 # mounted into the Flyway container
```

The default Flyway image is `docker.io/flyway/flyway:13`. The operator's `FLYWAY_IMAGE` environment variable
changes the default for all migrations, and `flywayImage` changes it for one migration.
Any Flyway setting not covered above can be set with `envVars`
(see [Flyway environment variables](https://documentation.red-gate.com/fd/environment-variables-184127165.html)).

## Job and pod metadata

`jobMetadata` sets labels and annotations on the Job, `podMetadata` on the Job's pod template. Use them for sidecar
and injector annotations, or for cost, team and NetworkPolicy labels. Nothing is copied from the `Migration`'s own
labels or annotations: an `app` label shared with your Deployment would let its Services, PodDisruptionBudgets and
NetworkPolicies select the migration pod, and GitOps tracking metadata would leak onto the Job.

The operator sets `app.kubernetes.io/managed-by: flyway-operator`, `app.kubernetes.io/name: flyway` and
`app.kubernetes.io/instance: <Migration name>` on both the Job and its pod template, and its own annotations on the Job.
These keys are reserved and a `Migration` using them in either map is rejected:

- keys under `flyway-operator.davidkarlsen.com/` and `batch.kubernetes.io/`
- `controller-uid` and `job-name`, which Kubernetes sets on Job pods
- `app.kubernetes.io/managed-by`, `app.kubernetes.io/name` and `app.kubernetes.io/instance`

Keys must be valid label or annotation keys, and label values valid label values (at most 63 characters).
Each map holds at most 64 entries.

### Istio

An Istio sidecar keeps running after Flyway exits, so the Job never completes. Disable injection for the migration pod:

```yaml
spec:
  podMetadata:
    annotations:
      sidecar.istio.io/inject: "false"
```

Istio in native sidecar mode (Kubernetes 1.29+) runs the proxy as a native sidecar, which does not keep the Job running.

Adding `podMetadata` does not unstick a Job that already hangs: the operator waits for a running Job of an older
generation to finish before it creates the new one, and a Job held by a sidecar never finishes. Delete it by hand:

```shell
kubectl delete job my-app
```

## What happens

- The operator creates a Job with the same name as the `Migration`, owned by the `Migration`.
- The Job is annotated with the `Migration`'s generation. A spec change creates a new Job; an unchanged spec does not rerun it.
  This includes `jobMetadata` and `podMetadata`: changing them re-runs the migration, which is a no-op for `migrate` when
  nothing is pending. Existing Jobs are never patched in place.
- The Job retries its pod twice (`backoffLimit: 2`) before it fails.
- A failed Job is re-created up to `spec.retryLimit` times (default `2`) for the same generation. After that the
  `Migration` reports `Failed=True` and no new Job starts until the spec changes. Deleting the failed Job starts one more attempt.

## Status

The `Migration` reports three conditions. Each carries the `observedGeneration` it was computed for; a condition with an
older generation does not describe the current spec. `status.observedGeneration` and `status.attempts` (Jobs started for
that generation) are also set.

| Condition | `True` when |
| --- | --- |
| `Applied` | the Job for the current generation succeeded |
| `Progressing` | a Job for the current generation is created, running, or being retried |
| `Failed` | the Job for the current generation failed and `retryLimit` is exhausted |

All three share one reason: `JobCreated`, `JobRunning`, `JobSucceeded`, `Retrying`, `RetryLimitExceeded`,
`PreviousJobRunning` (a Job for an older generation is still running) or `PreviousJobReplaced`. On failure the message
holds the Job's own failure reason and message. `ReconcileSuccess` only means the reconcile loop ran.

`status.lastAppliedImageRef` holds the `migrationSource.imageRef` of the last Job that succeeded, taken from that Job, and
`status.lastAppliedGeneration` the generation it ran for. Both keep their value while a later run is in progress or after
it fails. This is the image the SQL files came from, not the schema version in the database; for that, query Flyway's
schema history table, or read the Job's logs while the Job still exists.

For an Argo CD health check, see [Argo CD](argocd.md).

Check the result:

```shell
kubectl get migration my-app -o jsonpath='{.status.conditions}'
kubectl logs job/my-app -c flyway
```

## Pausing

Stop the operator from reconciling a `Migration`:

```shell
kubectl annotate migration my-app flyway-operator.davidkarlsen.com/paused=true
```

Remove the annotation, or set it to anything other than `true`, to resume.

## Examples

- [DB2 with username and password](examples/db2-password.md)
- [Azure SQL with a service principal and certificate](examples/mssql-sp-cert.md)
