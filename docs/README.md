# flyway-operator documentation

## What it does

The operator adds a `Migration` custom resource (`flyway.davidkarlsen.com/v1alpha1`).
For each `Migration` it runs a Kubernetes [Job](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
that applies your SQL with the [Flyway](https://documentation.red-gate.com/fd) container image:

- SQL scripts ship as a container image, mounted read-only as an image volume (Kubernetes 1.35 or later).
- Database credentials come from a Kubernetes `Secret`, or are left out for passwordless logins through the pod's workload identity.
- Changing the `Migration` spec starts a new Job. The result is reported as conditions on the `Migration` status.
- Deleting a `Migration` also deletes its Job.
- Reconciliation can be paused with an annotation.

## Why

Schema changes are declared next to their application and applied by the cluster, like the rest of a GitOps-managed
deployment. The application does not embed Flyway, run migrations at startup, or hold DDL privileges. The SQL is
versioned and promoted as an immutable image.

## Contents

- [Installing](installing.md): OpenShift OLM or Helm
- [Usage](usage.md): the `Migration` resource, status, pausing
- [Argo CD](argocd.md): health check and sync order
- [Metrics](metrics.md): secured `/metrics` endpoint, Prometheus scraping, RBAC, shipped alerts
- [Alert runbook](runbook.md): what each shipped alert means and how to fix it
- [Grafana dashboards](dashboards.md): operator health and Migration dashboards, grafana-operator and sidecar setup
- [Verifying releases](verifying.md): cosign signatures and build provenance of images and release assets
- Examples
  - [DB2 with username and password](examples/db2-password.md)
  - [Azure SQL with a service principal and certificate](examples/mssql-sp-cert.md)
  - [Resources, scheduling and service account (ResourceQuota)](examples/resources-and-quota.md)
  - [Azure SQL with Entra ID workload identity (passwordless)](examples/azure-sql-workload-identity.md)
