# Usage

After [installing](installing.md) the operator, create `Migration` resources in the namespace of your application.

## The migration source image

Put your Flyway SQL files (`V1__init.sql`, ...) in a container image, by default under `/sql`.
The Job mounts the image read-only as an [image volume](https://kubernetes.io/docs/concepts/storage/volumes/#image)
into the Flyway container, and Flyway reads the files from `migrationSource.path`.
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
    username: myuser                 # optional, passed as FLYWAY_USER
    credentials:                     # optional, Secret key passed as FLYWAY_PASSWORD; omit for passwordless login
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
changes the default for all migrations, and `flywayImage` for one. Set any other Flyway setting with `envVars`
([Flyway environment variables](https://documentation.red-gate.com/fd/environment-variables-184127165.html)).

## Job and pod metadata

`jobMetadata` sets labels and annotations on the Job, `podMetadata` on its pod template. Use them for sidecar and
injector annotations, or for cost, team and NetworkPolicy labels. Nothing is copied from the `Migration`'s own labels
or annotations: a shared `app` label would let your Deployment's Services, PodDisruptionBudgets and NetworkPolicies
select the migration pod, and GitOps tracking metadata would leak onto the Job.

The operator sets these labels on the Job and its pod template, and its own annotations on the Job:

| Label | Value |
| --- | --- |
| `app.kubernetes.io/managed-by` | `flyway-operator` |
| `flyway-operator.davidkarlsen.com/migration` | the `Migration`'s name |

Select migration pods, for example in a NetworkPolicy, on these labels. The operator sets no
`app.kubernetes.io/name` or `app.kubernetes.io/instance`, so selectors on a Helm release name don't match; set them yourself.

A `Migration` using a reserved key in either map is rejected:

- keys under `flyway-operator.davidkarlsen.com/` and `batch.kubernetes.io/`
- `controller-uid` and `job-name`, which Kubernetes sets on Job pods
- `app.kubernetes.io/managed-by`

Keys must be valid label or annotation keys, and label values valid label values (at most 63 characters). Each map
holds at most 64 entries. Kubernetes limits all annotations on an object to 256 KiB; the CRD does not check this, and a
Job rejected for it shows up in the `Migration`'s status and events.

### Istio

An Istio sidecar keeps running after Flyway exits, so the Job never completes. Disable injection for the pod:

```yaml
spec:
  podMetadata:
    annotations:
      sidecar.istio.io/inject: "false"
```

Istio in native sidecar mode (Kubernetes 1.29+) does not keep the Job running. Without either, the [deadline](#deadline)
ends the pod but reports a failure. Adding `podMetadata` does not unstick a Job that already hangs, see
[Hung Jobs](#hung-jobs).

## What happens

- The operator creates a Job with the `Migration`'s name, owned by the `Migration`. A name longer than 63 characters is
  rejected, because it is the Job name and a pod label value.
- The pod mounts the source image read-only at `/flyway/migration-source` and sets
  `FLYWAY_LOCATIONS=filesystem:/flyway/migration-source/<path>` and `FLYWAY_FAIL_ON_MISSING_LOCATIONS=true`, so a wrong
  `path` fails the Job instead of applying nothing. An `envVars` entry of the same name replaces either (for example to
  add locations).
- If the API server stores the Job without the image volume (Kubernetes older than 1.35, or the `ImageVolume` feature
  gate off), the operator deletes the Job and the `Migration` reports `Failed=True` with reason `ImageVolumeUnsupported`.
  Change the spec to try again.
- The Job is annotated with the `Migration`'s generation. A spec change creates a new Job, including a change of
  `jobMetadata`, `podMetadata` or `activeDeadline`; Flyway runs again, a no-op for `migrate` when nothing is pending.
  An unchanged spec does not rerun it. Existing Jobs are never patched in place.
- The Job retries its pod twice (`backoffLimit: 2`) before it fails.
- A failed Job is re-created up to `spec.retryLimit` times (default `2`) for the same generation. After that the
  `Migration` reports `Failed=True` and no new Job starts until the spec changes. Deleting the failed Job starts one more attempt.
- Each Job runs at most `spec.activeDeadline`, see [Deadline](#deadline).
- The pod runs with a [restricted security context](#security-context).

## Deadline

`spec.activeDeadline` is the maximum run time per Job attempt: a duration such as `30m`, between `1s` and `8760h`,
default `1h`. It becomes the Job's `activeDeadlineSeconds`, rounded up to whole seconds, and includes image pulls and the
pod's retries.

- At the deadline Kubernetes kills the pod and fails the Job with reason `DeadlineExceeded`. This counts against
  `retryLimit`, so a `Migration` can run up to `activeDeadline × (retryLimit + 1)` before it reports `Failed`. Set
  `retryLimit: 0` to not retry after a timeout.
- While retries remain the reason is `Retrying`. When the last attempt times out, the `Migration` reports `Failed=True`
  with reason `TimedOut` instead of `RetryLimitExceeded`, and a `TimedOut` Warning event instead of `Failed`. The message
  says `timed out (activeDeadline ...)` and holds the Job's `DeadlineExceeded` reason. Anything matching
  `RetryLimitExceeded` to detect a failed `Migration` must also match `TimedOut`, or match `Failed=True`.
- Killing Flyway mid-migration is only safe where DDL is transactional (PostgreSQL, SQL Server). On MySQL, Oracle and DB2 a
  killed migration can leave a script partly applied and a failed row in the schema history table, which needs
  `flyway repair` and manual cleanup. Prefer database-side limits that fail cleanly, such as `lock_timeout` /
  `statement_timeout` through `jdbcUrl` or `jdbcProperties`, and Flyway's `FLYWAY_CONNECT_RETRIES` and
  `FLYWAY_LOCK_RETRY_COUNT` (in `envVars`), and keep `activeDeadline` as the outer bound.
- The deadline only applies to Jobs created with it. A Job already hung keeps running, see [Hung Jobs](#hung-jobs).

## Resources, scheduling and service account

These optional `flywayConfiguration` fields shape the Job's pod. Unset, the pod has no requests or limits, uses the
default scheduler and runs as the namespace's `default` service account.

| Field | Applies to |
|---|---|
| `resources` | the Flyway container (`requests`, `limits`) |
| `nodeSelector`, `tolerations`, `affinity`, `priorityClassName` | the pod |
| `serviceAccountName` | the pod |

A complete, validated `Migration` with `resources`, `serviceAccountName`, `nodeSelector`, `tolerations` and
`priorityClassName` is in [examples/resources-and-quota.md](examples/resources-and-quota.md).

The operator sets no default `resources`. Flyway runs on the JVM, so the right size depends on your migrations; the
numbers above are a starting point, not a measured recommendation. `resources.claims` is rejected: the operator does
not define the pod's `resourceClaims` that a claim would refer to.

### ResourceQuota

A namespace with a `ResourceQuota` on `requests.*` or `limits.*` rejects pods that do not set them (unless a
`LimitRange` supplies defaults). Set `resources` for every resource the quota covers. Without it the Job cannot create
its pod, see [When the pod does not start](#when-the-pod-does-not-start).

`serviceAccountName` lets a `Migration` author run the pod as any service account of the namespace, including its
cloud identity. Grant `Migration` edit rights accordingly. Workload identity systems may also need a pod label or
annotation, set with `podMetadata` (for example `azure.workload.identity/use: "true"` for Azure Workload Identity).

A change to any of these fields creates a new Job, which runs the configured `commands` again. Helm does not update the
CRD of an existing install, see [Upgrading the CRD](installing.md#upgrading-the-crd).

## Passwordless authentication

`database.credentials` is optional. When unset, the operator does not set `FLYWAY_PASSWORD` and the JDBC driver
authenticates by other means: `jdbcUrl` and `flywayConfiguration.jdbcProperties` select the mode, and the pod's workload
identity supplies the credential. `database.username` is optional too; when unset, `FLYWAY_USER` is left out. Pair this
with `flywayConfiguration.serviceAccountName`, and with `podMetadata` where your identity system needs a pod label or
annotation. The snippets below show only the fields that matter; combine them with the `Migration` above.

What was checked against `docker.io/flyway/flyway:13` (Flyway 13.12.1) by listing its files, not by logging in:

- It bundles the Microsoft JDBC driver `mssql-jdbc-12.10.2` and, in `/flyway/lib/aad`, `azure-identity-1.18.5` and
  `msal4j-1.23.1`, the libraries the driver's Entra ID modes load.
- It bundles the PostgreSQL driver `postgresql-42.7.14`. It does not contain the AWS SDK or the AWS Advanced JDBC
  Wrapper, so the stock image cannot authenticate to RDS with IAM.

No authentication against Azure or AWS was run for this documentation. Everything below is "should work, verify in your
setup".

### Azure SQL with Entra ID

With [Azure Workload Identity](https://azure.github.io/azure-workload-identity/docs/): a service account annotated with
the identity's client ID, and the pod label that makes the webhook inject the token. `ActiveDirectoryDefault` makes the
driver use `DefaultAzureCredential`, which includes workload identity.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: flyway
  annotations:
    azure.workload.identity/client-id: "00000000-0000-0000-0000-000000000000"
```

The `Migration` needs the `authentication=ActiveDirectoryDefault` JDBC URL, the pod label
`azure.workload.identity/use: "true"` and `serviceAccountName: flyway`; a complete one is in
[examples/azure-sql-workload-identity.md](examples/azure-sql-workload-identity.md).

If the service account lacks the `azure.workload.identity/client-id` annotation or the pod lacks the label, no token is
injected and the Job fails with an Azure identity error from the driver in its logs (`kubectl logs job/my-app`).

On Azure Red Hat OpenShift the Azure Workload Identity webhook is not part of the platform (to verify for your
cluster): install it, or project the service account token yourself, and create a federated credential for the
identity against the cluster's service account issuer.

`authentication=ActiveDirectoryManagedIdentity` (add `msiClientId=<client id>` for a user-assigned identity) uses the
node's managed identity through IMDS and needs no service account. Every pod on that node can then reach the database
with that identity, so prefer workload identity. On AKS, with several identities on the node pool, `msiClientId` is
effectively required.

With either mode the identity needs a database user, created by an Entra admin of the database, with the rights a
password user would have. Flyway must read and write `flyway_schema_history` and run your DDL; `db_owner` is often what
it takes:

```sql
CREATE USER [my-identity] FROM EXTERNAL PROVIDER;
ALTER ROLE db_datareader ADD MEMBER [my-identity];
ALTER ROLE db_datawriter ADD MEMBER [my-identity];
ALTER ROLE db_ddladmin ADD MEMBER [my-identity];
```

### AWS RDS with IAM

RDS IAM authentication needs the [AWS Advanced JDBC Wrapper](https://github.com/aws/aws-advanced-jdbc-wrapper) and its
dependencies in `/flyway/drivers` of a custom image (`flywayConfiguration.flywayImage`). The service account is bound to
an IAM role (IRSA or EKS Pod Identity) that allows `rds-db:connect`. `username` is the database user, created with
`GRANT rds_iam TO` on PostgreSQL. With IRSA the wrapper's IAM plugin also needs `software.amazon.awssdk:sts` on the
classpath (to verify).

```yaml
spec:
  database:
    username: flyway_iam
    jdbcUrl: "jdbc:aws-wrapper:postgresql://mydb.abc123.eu-west-1.rds.amazonaws.com:5432/mydb?wrapperPlugins=iam"
  flywayConfiguration:
    serviceAccountName: flyway
```

Whether Flyway accepts the `jdbc:aws-wrapper:` URL with the wrapper jar on the driver path has not been checked.

## Security context

By default the pod complies with the `restricted`
[Pod Security Standard](https://kubernetes.io/docs/concepts/security/pod-security-standards/), so it is admitted in
namespaces labelled `pod-security.kubernetes.io/enforce=restricted`:

- Pod: `runAsNonRoot: true`, `seccompProfile: RuntimeDefault`, `fsGroupChangePolicy: OnRootMismatch`.
  - On OpenShift (detected once at operator startup from the `security.openshift.io` API), the SCC assigns the UID and
    fsGroup.
  - Elsewhere the pod sets `runAsUser: 1001` and `fsGroup: 1001`.
- The `flyway` container: `allowPrivilegeEscalation: false`, all capabilities dropped, `readOnlyRootFilesystem: true`.
  The source image volume is always read-only.
- An `emptyDir` volume named `flyway-operator-tmp` is mounted at `/tmp` in the `flyway` container, because JDBC drivers
  such as SQLite unpack native libraries into it. The name is reserved: a `volumes` entry of that name is used instead.
  A container that already mounts something at `/tmp` keeps its own mount.

What your images and volumes must allow:

- The Flyway image must run as a UID that does not own its files, with group `0` (OpenShift) or `1001` as fsGroup, and
  write only to `/tmp` or mounted volumes. Its files must be world-readable. The Flyway image meets this.
- The SQL files in the source image must be readable by that UID, for example world-readable.
- Volumes in `volumes`/`volumeMounts` must be readable, or writable where needed, by that user. Kubernetes gives the
  fsGroup group access to Secret, ConfigMap, emptyDir and most persistent volumes (including a Secret with
  `defaultMode: 0400`); volume types that ignore fsGroup, such as `hostPath` or some NFS servers, must allow the UID.

### Overriding

`flywayConfiguration.podSecurityContext` replaces the pod default and `flywayConfiguration.securityContext` the default
of the Flyway container. Each replaces its default as a whole, so repeat every field you still want. For a Flyway image
that needs a writable root filesystem, keep the rest of the restricted container context:

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

The `Migration` stays `Progressing` with reason `JobRunning`, but while no container of the current Job's pod has
started, the operator puts the cause into the `Progressing` message and looks again every 15 seconds:

```shell
kubectl get migration my-app -o jsonpath='{.status.conditions[?(@.type=="Progressing")].message}'
# Job my-app attempt 1 of 3 is running; pod not created: pods "my-app-" is forbidden: exceeded quota: ...
```

| Message after "is running;" | Source | Cause and fix |
| --- | --- | --- |
| `pod not created: ...` | the newest `FailedCreate` event of the Job | Admission rejected the pod, see below. |
| `no pod created yet, see kubectl describe job` | the Job is over 30s old and no `FailedCreate` event can be read | As above; the operator has no RBAC for events (see below) or the event expired. |
| `pod unschedulable: ...` | the pod's `PodScheduled` condition (`FailedScheduling`; `kubectl describe pod` shows it, the Job does not) | `nodeSelector`, `affinity`, `tolerations` or `resources` fit no node. Fix them. |
| `image pull failed: ...` | `ImagePullBackOff`, `ErrImagePull` or `InvalidImageName` of a container | Wrong image or registry credentials. |
| `container cannot start: ...` | `CreateContainerConfigError` or `CreateContainerError` | For example a missing Secret, or `container has runAsNonRoot and image will run as root`: no UID was assigned (for example by `anyuid`) and the image has no non-root `USER`. Set `podSecurityContext.runAsUser`. |

`FailedCreate` causes and fixes:

| `FailedCreate` message | Cause and fix |
| --- | --- |
| `exceeded quota` or `must specify limits.cpu` | A `ResourceQuota` and no `resources`: see [ResourceQuota](#resourcequota). |
| `serviceaccount "..." not found` or `no PriorityClass with name ... was found` | `serviceAccountName` or `priorityClassName` names something that does not exist. Creating it is enough, since the Job controller keeps retrying; correcting the `Migration` works too (see below). |
| `violates PodSecurity "restricted"` or `unable to validate against any security context constraint` | An override dropped part of the restricted context, the image needs a UID the SCC does not allow, or OpenShift chose an SCC that assigns no UID. Pin the SCC with the pod annotation below. |

```yaml
spec:
  podMetadata:
    annotations:
      openshift.io/required-scc: restricted-v2
```

Messages are cut at 300 characters, so `kubectl describe` still has the full text. The operator needs `list` on `pods`
and on the Job's `events`; kustomize and OLM grant it, for Helm see [Before you upgrade](installing.md#before-you-upgrade).
A missing permission degrades differently:

- **`events` forbidden:** the cause of a missing pod is not shown; the message says `no pod created yet`.
- **`pods` forbidden:** the message is the plain `is running` text, and the operator cannot tell whether an old Job ever
  started, so it keeps waiting for it (`PreviousJobRunning`) and never does the replacement described below.

A failure to pull the *migration source* image volume is not named: the pod shows `ContainerCreating` and the reason is
in `kubectl describe pod`. Look at the details by hand:

```shell
kubectl describe job my-app     # FailedCreate: the pod was not admitted
kubectl describe pod -l batch.kubernetes.io/job-name=my-app   # CreateContainerConfigError and container errors
```

Failed pod creations do not count against the Job's `backoffLimit`, so such a Job does not fail by itself. The
[deadline](#deadline) bounds it once per attempt: with the defaults (`activeDeadline` 1h, `retryLimit` 2) a pod that
can never be created takes 3 hours to report `TimedOut`, because each retry hits the same cause.

Correcting the spec does not need a manual delete. When the `Migration` changes while an older generation's Job exists
and **no Flyway container of it ever started** (it has no pod, or only `Pending` pods, and its status shows no pod that
ran or ended), the operator deletes that Job with foreground propagation and creates the new generation's Job once the
old pods are gone (condition reason `PreviousJobReplaced`, a `PreviousJobReplaced` event; it counts no run in the
metrics, since nothing ran). A Job whose Flyway container started, or whose pods cannot be listed, is still waited for
(`PreviousJobRunning`), so two migrations never run against the database at once.

### Hung Jobs

A Job whose Flyway container started and hangs (for example behind an Istio sidecar) blocks later spec changes, because
the operator waits for a running Job of an older generation (`PreviousJobRunning`). Fix the cause and delete the Job by
hand to start the new one at once:

```shell
kubectl delete job my-app
```

## Status

The `Migration` reports three conditions. Each carries the `observedGeneration` it was computed for; a condition with an
older generation does not describe the current spec. `status.observedGeneration` and `status.attempts` (Jobs started for
that generation) are also set.

| Condition | `True` when |
| --- | --- |
| `Applied` | the Job for the current generation succeeded |
| `Progressing` | a Job for the current generation is created, running, or being retried |
| `Failed` | the Job for the current generation failed and `retryLimit` is exhausted, or the cluster dropped its image volume |

All three share one reason: `JobCreated`, `JobRunning`, `JobSucceeded`, `Retrying` (an attempt failed and another
follows), `RetryLimitExceeded` (the last attempt failed), `TimedOut` (the last attempt exceeded `spec.activeDeadline`,
see [Deadline](#deadline)), `PreviousJobRunning` (a Job for an older generation is still running), `PreviousJobReplaced`
(an older Job was replaced, also one that never started a container) or `ImageVolumeUnsupported` (the cluster dropped
the image volume).

On failure the message holds the Job's own failure reason and message, followed by Flyway's error when it can be read
(see below).

A reconcile that fails (for example the API server rejects the Job) also sets a `ReconcileError` condition with the
error as its message, and emits a `ProcessingError` Warning event. The next successful reconcile removes the condition.

`status.lastAppliedImageRef` holds the `migrationSource.imageRef` of the last Job that succeeded, taken from that Job, and
`status.lastAppliedGeneration` the generation it ran for. Both keep their value while a later run is in progress or after
it fails. This is the image the SQL files came from, not the schema version in the database.

`status.schemaVersion` is the database's Flyway schema version as the last succeeded Job reported it, and
`kubectl get migration` shows it in the `SCHEMA VERSION` column. It comes from the last `info` or `migrate` result in
the Flyway output (`info`: `schemaVersion`; `migrate`: the version it migrated to, or the version it started from when
nothing was pending). Like `lastAppliedImageRef`, it keeps its value while a later run is in progress or after it fails,
and also when a run reports no version (for example `commands: [validate]`, or `info` on an empty schema). Commands such
as `clean`, `undo` or `baseline` and a changed `jdbcUrl` can therefore leave a value that no longer matches the database.
A failed run on a database without transactional DDL (for example MySQL or Oracle) can leave a migration partly applied;
`status.schemaVersion` does not show that until the next run succeeds.

When a Job fails, Flyway's error message is appended to the `Retrying`, `RetryLimitExceeded` or `TimedOut` message
after `; flyway: `, cut at 300 characters, without Flyway's separator lines and its `Statement` line:

```shell
kubectl get migration my-app -o jsonpath='{.status.conditions[?(@.type=="Failed")].message}'
# Job my-app failed on attempt 3 of 3: BackoffLimitExceeded: Job has reached the specified backoff limit; flyway: Failed to execute script V2__add_column.sql SQL State : 42601 ...
```

The text names the failing script, the database's error and, for connection errors, the user and a masked JDBC URL.
The database's error can quote values from the failing statement or row. Anyone who can read the `Migration` can read
it, not only those who can read the pod's log.

Both come from the log of the Flyway container of the Job's newest pod (`pods/log`; the operator always runs Flyway
with `-outputType=json`), read once when the Job finishes, at most 4 MiB from its start and for at most 10 seconds.
Reading the log is best effort and never changes the result: when the log cannot be read (no RBAC, the pod is gone, a
timeout) or holds no complete Flyway JSON result (an `envVars` or command override of the output type, a log over
4 MiB), `status.schemaVersion` keeps its value and the message carries no Flyway error. The JSON comes at the end of the
log, and every `info` lists each migration with about 450 bytes, so the 4 MiB limit is reached at roughly 4000 migrations
with the default `info migrate info`, or sooner with debug output (`-X`). The operator needs `get` on `pods/log`, which
lets it read the logs of every pod it can see. Kustomize and OLM grant it; for Helm see
[Before you upgrade](installing.md#before-you-upgrade). Migrations that succeeded before the upgrade get a
`schemaVersion` with their next run.

Check the result:

```shell
kubectl get migration my-app -o jsonpath='{.status.conditions}'
kubectl logs job/my-app -c flyway
```

For an Argo CD health check, see [Argo CD](argocd.md).

## Pausing

Stop the operator from reconciling a `Migration`:

```shell
kubectl annotate migration my-app flyway-operator.davidkarlsen.com/paused=true
```

Remove the annotation, or set it to anything other than `true`, to resume.

## Examples

- [DB2 with username and password](examples/db2-password.md)
- [Azure SQL with a service principal and certificate](examples/mssql-sp-cert.md)
