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
    jdbcUrl: "jdbc:..."              # must start with jdbc:
  retryLimit: 2                      # optional, default 2: re-creations of a failed Job
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
```

The default Flyway image is `docker.io/flyway/flyway:13`. The operator's `FLYWAY_IMAGE` environment variable
changes the default for all migrations, and `flywayImage` changes it for one migration.
Any Flyway setting not covered above can be set with `envVars`
(see [Flyway environment variables](https://documentation.red-gate.com/fd/environment-variables-184127165.html)).

## What happens

- The operator creates a Job with the same name as the `Migration`, owned by the `Migration`. Its pod mounts the
  migration source image read-only at `/flyway/migration-source` and sets
  `FLYWAY_LOCATIONS=filesystem:/flyway/migration-source/<path>` and `FLYWAY_FAIL_ON_MISSING_LOCATIONS=true`, so a wrong
  `path` fails the Job instead of applying nothing. Both are set before `envVars`, so an entry there replaces them
  (for example to add more locations).
- If the API server stores the Job without the image volume (Kubernetes older than 1.35, or the `ImageVolume` feature
  gate off), the operator deletes the Job and the `Migration` reports `Failed=True` with reason `ImageVolumeUnsupported`.
  Change the spec to try again.
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
`PreviousJobRunning` (a Job for an older generation is still running), `PreviousJobReplaced` or
`ImageVolumeUnsupported` (the cluster dropped the image volume). On failure the message
holds the Job's own failure reason and message. `ReconcileSuccess` only means the reconcile loop ran.

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
