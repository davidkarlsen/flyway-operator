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
- The Job retries twice (`backoffLimit: 2`) before it fails.

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
