# DB2 with username and password

The Flyway image includes the DB2 JDBC driver, so no extra configuration is needed.

```shell
kubectl create secret generic migration-pw -n my-app --from-literal=password='<db2 password>'
```

```yaml
apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: my-app
  namespace: my-app
spec:
  database:
    username: db2inst1
    credentials:
      name: migration-pw
      key: password
    jdbcUrl: "jdbc:db2://db2.example.com:50000/DEVDB"
  migrationSource:
    imageRef: "ghcr.io/davidkarlsen/testmigration:latest"
  flywayConfiguration:
    defaultSchema: MYAPP
```

To connect with TLS, add `:sslConnection=true;` to the end of the URL
(`jdbc:db2://db2.example.com:50001/DEVDB:sslConnection=true;`). If the server certificate is not signed by a
public CA, mount the truststore with `flywayConfiguration.volumes`/`volumeMounts` and set `sslTrustStoreLocation`
and `sslTrustStorePassword` in the URL.
