# Azure SQL with Entra ID workload identity (passwordless)

> **Untested.** The Flyway image bundles the Microsoft JDBC driver and the Azure identity libraries, but no login
> with this example has been run against Azure. See
> [Passwordless authentication](../usage.md#passwordless-authentication) for what was checked.

No `credentials` and no `username`: the JDBC driver authenticates as the pod's workload identity.

```yaml
apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: my-app-passwordless
  namespace: my-app
spec:
  database:
    jdbcUrl: "jdbc:sqlserver://myserver.database.windows.net:1433;database=mydb;encrypt=true;authentication=ActiveDirectoryDefault"
  podMetadata:
    labels:
      azure.workload.identity/use: "true"
  migrationSource:
    imageRef: "registry.example.com/my-app-sql:1.4.0"
  flywayConfiguration:
    defaultSchema: dbo
    serviceAccountName: flyway
```
