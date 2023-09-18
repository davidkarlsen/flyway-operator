# Creating migrations

After [installing](INSTALLING.md) the operator you can declare your migrations.


Create a migration:
```yaml
apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  # add whatever labels you find useful, none are required
  labels:
    app.kubernetes.io/name: migration
    app.kubernetes.io/instance: migration-sample
    app.kubernetes.io/part-of: flyway-operator
    app.kubernetes.io/managed-by: kustomize
    app.kubernetes.io/created-by: flyway-operator
  name: migration-sample
  namespace: default
spec:
  database:
    # the database username
    username: db2inst1
    # a reference to a secret named `migration-pw` having a key `password` which contains the password for the user
    credentials:
      name: migration-pw
      key: password
    # a JDBC-url to connect to the database
    jdbcUrl: "jdbc:db2://somehost:50000/devdb"
  migrationSource:
    # this is a docker-image containing flyway-migrations in /sql
    # note - the image needs to contain the `cp` command
    imageRef: "ghcr.io/davidkarlsen/testmigration:latest"
    # the rest of the params are optional and can be left out
    # optional, reference to path containing the SQLs for the migrations within image, default is /sql
    path: "/sql"
    # optional, the file-encoding of the SQLs to migrate, default is UTF-8
    encoding: "UTF-8"
    # optional, override the flyway-image, for instance to use a pre-baked image containing non-default database-drivers. Default is the latest v9 image from docker-hub.
    flywayImage: ghcr.io/davidkarlsen/flyway-db2:9.22
```