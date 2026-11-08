# Resources, scheduling and service account

Sets the Flyway container's resources and the pod's scheduling fields and service account, as needed in a namespace
with a `ResourceQuota`. See [Resources, scheduling and service account](../usage.md#resources-scheduling-and-service-account).
This example is validated against the CRD by the repository's tests.

```yaml
apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: my-app-resources
  namespace: my-app
spec:
  database:
    username: myuser
    credentials:
      name: my-app-db
      key: password
    jdbcUrl: "jdbc:postgresql://my-db:5432/my_app"
  migrationSource:
    imageRef: "registry.example.com/my-app-sql:1.4.0"
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
