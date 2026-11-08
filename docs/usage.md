# Usage

After [installing](installing.md) the operator, create `Migration` resources in the namespace of your application.

## The migration source image

Put your Flyway SQL files (`V1__init.sql`, ...) in a container image, by default under `/sql`.
The Job mounts the image read-only as an [image volume](https://kubernetes.io/docs/concepts/storage/volumes/#image)
into the Flyway container, and Flyway reads the SQL files from `migrationSource.path` inside it.
The image needs no shell or other tools. A minimal image:

```dockerfile
FROM scratch
COPY sql/ /sql/
```

The image is pulled with `pullPolicy: Always`. Image volumes need Kubernetes 1.35 or later, see [installing](installing.md#requirements).

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
  activeDeadline: 1h                 # optional, default 1h: maximum run time per Job attempt
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
    path: /sql                       # optional, default /sql: SQL location inside the image, no ".."
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
    # podSecurityContext: {}         # optional: replaces the default as a whole, see Security context
    # securityContext: {}            # optional: replaces the default as a whole, for the Flyway container
    # resources: {}                  # optional: requests/limits of the Flyway container, unset by default
    # serviceAccountName: flyway     # optional: the pod's service account, default the namespace's default
    # nodeSelector: {}               # optional, pod scheduling: also tolerations, affinity, priorityClassName
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

The operator sets these labels on both the Job and its pod template, and its own annotations on the Job:

| Label | Value |
| --- | --- |
| `app.kubernetes.io/managed-by` | `flyway-operator` |
| `flyway-operator.davidkarlsen.com/migration` | the `Migration`'s name |

Select migration pods, e.g. in a NetworkPolicy, on these labels. The operator does not set `app.kubernetes.io/name` or
`app.kubernetes.io/instance`, so selectors on a Helm release name don't match migration pods; you can set them yourself.

These keys are reserved and a `Migration` using them in either map is rejected:

- keys under `flyway-operator.davidkarlsen.com/` and `batch.kubernetes.io/`
- `controller-uid` and `job-name`, which Kubernetes sets on Job pods
- `app.kubernetes.io/managed-by`

Keys must be valid label or annotation keys, and label values valid label values (at most 63 characters).
Each map holds at most 64 entries. Kubernetes limits all annotations on an object to 256 KiB in total; the CRD does not
check this, and a Job rejected for it shows up in the `Migration`'s status and events.

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

- The operator creates a Job with the same name as the `Migration`, owned by the `Migration`. A `Migration` name
  longer than 63 characters is rejected, because the name is used as the Job name and as a pod label value.
- The Job's pod mounts the
  migration source image read-only at `/flyway/migration-source` and sets
  `FLYWAY_LOCATIONS=filesystem:/flyway/migration-source/<path>` and `FLYWAY_FAIL_ON_MISSING_LOCATIONS=true`, so a wrong
  `path` fails the Job instead of applying nothing. An entry with the same name in `envVars` replaces either one
  (for example to add more locations).
- If the API server stores the Job without the image volume (Kubernetes older than 1.35, or the `ImageVolume` feature
  gate off), the operator deletes the Job and the `Migration` reports `Failed=True` with reason `ImageVolumeUnsupported`.
  Change the spec to try again.
- The Job is annotated with the `Migration`'s generation. A spec change creates a new Job; an unchanged spec does not rerun it.
  This includes `jobMetadata` and `podMetadata`: changing them re-runs the migration, which is a no-op for `migrate` when
  nothing is pending. Existing Jobs are never patched in place.
- The Job retries its pod twice (`backoffLimit: 2`) before it fails.
- The Job's pod runs with a [restricted security context](#security-context).
- A failed Job is re-created up to `spec.retryLimit` times (default `2`) for the same generation. After that the
  `Migration` reports `Failed=True` and no new Job starts until the spec changes. Deleting the failed Job starts one more attempt.
- Each Job runs for at most `spec.activeDeadline` (default `1h`), see [Deadline](#deadline).

## Deadline

`spec.activeDeadline` is the maximum run time per Job attempt: a duration such as `30m`, between `1s` and `8760h`,
default `1h`. It becomes the Job's `activeDeadlineSeconds`, rounded up to whole seconds, and includes image pulls and the
Job's pod retries. A migration that legitimately runs longer than an hour must set a higher value.

- When the deadline is reached, Kubernetes kills the pod and fails the Job with reason `DeadlineExceeded`. This counts
  against `retryLimit` like any other failure, so a `Migration` can run for up to `activeDeadline × (retryLimit + 1)`
  before it reports `Failed`. Set `retryLimit: 0` to not retry after a timeout.
- While retries remain the reason is `Retrying`. When the last attempt times out, the `Migration` reports `Failed=True`
  with reason `TimedOut` instead of `RetryLimitExceeded`, and a `TimedOut` Warning event instead of `Failed`. The
  message says `timed out (activeDeadline ...)` and holds the Job's `DeadlineExceeded` reason. Anything matching
  `RetryLimitExceeded` to detect a failed `Migration` must also match `TimedOut`, or match `Failed=True` instead.
- Killing Flyway mid-migration is only safe where DDL is transactional (PostgreSQL, SQL Server). On MySQL, Oracle and DB2 a
  killed migration can leave a script partly applied and a failed row in the schema history table, which needs
  `flyway repair` and manual cleanup. Prefer database-side limits that fail cleanly, such as `lock_timeout` /
  `statement_timeout` set through `jdbcUrl` or `jdbcProperties`, and Flyway's `FLYWAY_CONNECT_RETRIES` and
  `FLYWAY_LOCK_RETRY_COUNT` (in `envVars`), and keep `activeDeadline` as the outer bound.
- Changing only `activeDeadline` is a spec change: it starts a new Job, which runs Flyway again (a no-op when nothing is
  pending).
- The deadline only applies to Jobs created with it. A Job that is already hung keeps running and blocks later spec
  changes (`PreviousJobRunning`); delete it with `kubectl delete job <name>`.
- With Istio, an injected sidecar that never exits keeps the pod running after Flyway has finished. The deadline bounds
  this but reports it as a failure; disable injection for the Job's pod (`podMetadata` annotation
  `sidecar.istio.io/inject: "false"`) or run the proxy as a native sidecar so the pod completes.

## Resources, scheduling and service account

These optional `flywayConfiguration` fields shape the Job's pod. Unset, the pod has no requests or limits, uses the
default scheduler and runs as the namespace's `default` service account.

| Field | Applies to |
|---|---|
| `resources` | the Flyway container (`requests`, `limits`) |
| `nodeSelector`, `tolerations`, `affinity`, `priorityClassName` | the pod |
| `serviceAccountName` | the pod |

```yaml
  flywayConfiguration:
    resources:
      requests: {cpu: 100m, memory: 256Mi}
      limits: {memory: 512Mi}
    serviceAccountName: flyway
    nodeSelector:
      kubernetes.io/os: linux
    tolerations:
      - {key: dedicated, operator: Equal, value: batch, effect: NoSchedule}
    priorityClassName: batch-low
```

The operator sets no default `resources`. Flyway runs on the JVM, so the right size depends on your migrations; the
numbers above are a starting point, not a measured recommendation.

### ResourceQuota

A namespace with a `ResourceQuota` on `requests.*` or `limits.*` rejects pods that do not set them (unless a
`LimitRange` supplies defaults). The Job then creates no pod, and the `Migration` stays `Progressing` until
`activeDeadline`. `kubectl describe job <name>` shows the `FailedCreate` event ("must specify limits.cpu ..."). Set
`resources` with every resource the quota covers. `serviceAccountName` must name an existing service account in the
namespace, or the Job cannot create its pod either. A Job whose pod cannot be created is not marked failed: after
fixing the field, delete the Job (`kubectl delete job <name>`) to start over at once instead of waiting for the
`activeDeadline` of the stuck one.

`serviceAccountName` lets a `Migration` author run the pod as any service account of the namespace, including its
cloud identity. Grant `Migration` edit rights accordingly. Workload identity systems may also need a pod label or
annotation, set with `podMetadata` (for example `azure.workload.identity/use: "true"` for Azure Workload Identity).

A change to any of these fields creates a new Job, which runs the configured `commands` again.

If you install with Helm, upgrade the CRD yourself (`kubectl apply --server-side -f` the CRD of the release): Helm does
not update CRDs in `crds/`, and the old schema drops the new fields silently.

## Security context

By default the Job's pod complies with the `restricted`
[Pod Security Standard](https://kubernetes.io/docs/concepts/security/pod-security-standards/), so it is admitted in
namespaces labelled `pod-security.kubernetes.io/enforce=restricted`:

- Pod: `runAsNonRoot: true`, `seccompProfile: RuntimeDefault`, `fsGroupChangePolicy: OnRootMismatch`.
  - On OpenShift (detected once at operator startup from the `security.openshift.io` API), the SCC assigns the UID and
    fsGroup.
  - Elsewhere the pod sets `runAsUser: 1001` and `fsGroup: 1001`.
- The `flyway` container: `allowPrivilegeEscalation: false`, all capabilities dropped, `readOnlyRootFilesystem: true`.
  The migration source image volume is read-only either way: image volumes are always mounted read-only.
- An `emptyDir` volume named `flyway-operator-tmp` is mounted at `/tmp` in the `flyway` container, because JDBC drivers such as
  SQLite unpack native libraries into it. The name is reserved: if `volumes` already defines `flyway-operator-tmp`,
  that volume is used instead. A container that already mounts something at `/tmp` keeps its own mount.

What your images and volumes must allow:

- The Flyway image must run as a UID that does not own its files, with group `0` (OpenShift) or `1001` as fsGroup, and
  write only to `/tmp` or mounted volumes. Its files must be world-readable. The Flyway image meets this.
- The SQL files in the migration source image must be readable by that UID, for example world-readable.
- Volumes in `volumes`/`volumeMounts` must be readable, or writable where needed, by that user. Kubernetes gives the
  fsGroup group access to Secret, ConfigMap, emptyDir and most persistent volumes (including a Secret with
  `defaultMode: 0400`); volume types that ignore fsGroup, such as `hostPath` or some NFS servers, must allow the UID.

### Overriding

`flywayConfiguration.podSecurityContext` replaces the pod default and `flywayConfiguration.securityContext` the default
of the Flyway container. Each replaces its default as a whole, so repeat every field you still want. For example, for a
Flyway image that needs a writable root filesystem, keep the rest of the restricted container context:

```yaml
  flywayConfiguration:
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: [ALL]
      readOnlyRootFilesystem: false
```

An image that must run as root needs `podSecurityContext: {runAsUser: 0}` and a namespace that does not enforce
`restricted`; on OpenShift, also an SCC such as `anyuid` granted to the Job's service account. The `anyuid` SCC assigns
no UID, so an image without a non-root `USER` fails `runAsNonRoot` there unless `podSecurityContext.runAsUser` is set.

### When the pod does not start

The `Migration` then stays `Progressing` with reason `JobCreated` or `JobRunning`. Look at the events:

```shell
kubectl describe job my-app     # FailedCreate: the pod was not admitted
kubectl describe pod -l batch.kubernetes.io/job-name=my-app   # CreateContainerConfigError and container errors
```

- `FailedCreate` with `violates PodSecurity "restricted"` or `unable to validate against any security context constraint`:
  an override dropped part of the restricted context, the image needs a UID the SCC does not allow, or OpenShift chose
  an SCC that assigns no UID. Pin the SCC with a pod annotation:

  ```yaml
  spec:
    podMetadata:
      annotations:
        openshift.io/required-scc: restricted-v2
  ```

- `CreateContainerConfigError` with `container has runAsNonRoot and image will run as root`: no UID was assigned
  (for example by `anyuid`) and the image has no non-root `USER`. Set `podSecurityContext.runAsUser`.

Failed pod creations do not count against the Job's `backoffLimit`, so such a Job does not fail by itself;
`spec.activeDeadline` bounds how long it runs. Fix the cause, then change the spec or delete the Job to start a new one.

## Status

The `Migration` reports three conditions. Each carries the `observedGeneration` it was computed for; a condition with an
older generation does not describe the current spec. `status.observedGeneration` and `status.attempts` (Jobs started for
that generation) are also set.

| Condition | `True` when |
| --- | --- |
| `Applied` | the Job for the current generation succeeded |
| `Progressing` | a Job for the current generation is created, running, or being retried |
| `Failed` | the Job for the current generation failed and `retryLimit` is exhausted, or the cluster dropped its image volume |

All three share one reason: `JobCreated`, `JobRunning`, `JobSucceeded`, `Retrying`, `RetryLimitExceeded`, `TimedOut`
(the last attempt exceeded `spec.activeDeadline`), `PreviousJobRunning` (a Job for an older generation is still
running), `PreviousJobReplaced` or `ImageVolumeUnsupported` (the cluster dropped the image volume). On failure the message
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
