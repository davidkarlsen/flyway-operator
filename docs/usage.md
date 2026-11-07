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
    jdbcUrl: "jdbc:..."              # must start with jdbc:
  retryLimit: 2                      # optional, default 2: re-creations of a failed Job
  migrationSource:
    imageRef: "registry.example.com/my-app-sql:1.4.0"
    path: /sql                       # optional, default /sql
    encoding: UTF-8                  # optional, default UTF-8
    ImagePullSecret:                 # optional
      - name: my-registry-pull
    placeholders:                    # optional, Flyway placeholders
      tablespace: USERSPACE1
  flywayConfiguration:               # all fields optional
    flywayImage: docker.io/flyway/flyway:10
    commands: [info, migrate, info]  # default
    defaultSchema: MYSCHEMA
    baselineOnMigrate: true
    jdbcProperties: {}               # passed as Flyway jdbcProperties
    envVars: []                      # extra env vars for the Flyway container
    volumes: []                      # extra volumes for the Job
    volumeMounts: []                 # mounted into the Flyway container
```

The default Flyway image is `docker.io/flyway/flyway:10`. The operator's `FLYWAY_IMAGE` environment variable
changes the default for all migrations, and `flywayImage` changes it for one migration.
Any Flyway setting not covered above can be set with `envVars`
(see [Flyway environment variables](https://documentation.red-gate.com/fd/environment-variables-184127165.html)).

## What happens

- The operator creates a Job with the same name as the `Migration`, owned by the `Migration`.
- The Job is annotated with the `Migration`'s generation. A spec change creates a new Job; an unchanged spec does not rerun it.
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
it fails. This is the image the SQL files came from, not the schema version in the database; for that, see the Job's
`flyway info` output.

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
