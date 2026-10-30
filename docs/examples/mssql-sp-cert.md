# Azure SQL with a service principal and certificate

> **Untested.** This example follows the Microsoft JDBC driver documentation but has not been run
> against a real database with this operator. Please report back (issue or PR) if it works for you,
> or what had to change.

The service principal signs in with a certificate instead of a client secret, using the Microsoft JDBC driver's
[`ActiveDirectoryServicePrincipalCertificate`](https://learn.microsoft.com/sql/connect/jdbc/connecting-using-azure-active-directory-authentication)
authentication mode.

## Prerequisites

- An Entra ID app registration / service principal with a certificate credential.
- A database user for the service principal, created by an Entra admin of the database:

  ```sql
  CREATE USER [my-app-migrator] FROM EXTERNAL PROVIDER;
  ALTER ROLE db_ddladmin ADD MEMBER [my-app-migrator];
  ```

- A Flyway image containing the libraries the driver needs for Entra ID authentication
  (`msal4j` and its dependencies). If the default image does not include them, build your own image on top of
  `flyway/flyway` and set it with `flywayConfiguration.flywayImage`.

## Secrets

The certificate and its private key, as a PFX file:

```shell
kubectl create secret generic my-app-sp-cert -n my-app --from-file=sp.pfx=./sp.pfx
kubectl create secret generic my-app-sp-cert-pw -n my-app --from-literal=password='<pfx password>'
```

## Migration

`username` is the service principal's application (client) ID. The operator always passes `credentials` as
`FLYWAY_PASSWORD`; in this mode the driver uses the password to unlock the PFX file.

```yaml
apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: my-app
  namespace: my-app
spec:
  database:
    username: "00000000-0000-0000-0000-000000000000"   # client ID
    credentials:
      name: my-app-sp-cert-pw
      key: password
    jdbcUrl: "jdbc:sqlserver://myserver.database.windows.net:1433;database=mydb;encrypt=true;authentication=ActiveDirectoryServicePrincipalCertificate;clientCertificate=/certs/sp.pfx"
  migrationSource:
    imageRef: "registry.example.com/my-app-sql:1.4.0"
  flywayConfiguration:
    defaultSchema: dbo
    volumes:
      - name: sp-cert
        secret:
          secretName: my-app-sp-cert
    volumeMounts:
      - name: sp-cert
        mountPath: /certs
        readOnly: true
```

If the certificate and private key are separate PEM files, mount both and use
`clientCertificate=/certs/tls.crt;clientKey=/certs/tls.key` in the URL. Set `clientKeyPassword` if the key is encrypted.
