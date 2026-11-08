# Flyway operator

**Database schema migrations as a Kubernetes resource.** Describe the migration your application needs, and the
operator runs it with [Flyway](https://flywaydb.org/), reports the result, and runs it again only when something changes.

**Build and quality**
[![build](https://github.com/davidkarlsen/flyway-operator/actions/workflows/build.yaml/badge.svg?branch=main)](https://github.com/davidkarlsen/flyway-operator/actions/workflows/build.yaml)
[![CodeQL](https://github.com/davidkarlsen/flyway-operator/actions/workflows/codeql.yaml/badge.svg?branch=main)](https://github.com/davidkarlsen/flyway-operator/actions/workflows/codeql.yaml)
[![codecov](https://codecov.io/gh/davidkarlsen/flyway-operator/branch/main/graph/badge.svg)](https://codecov.io/gh/davidkarlsen/flyway-operator)
[![chart release](https://img.shields.io/github/actions/workflow/status/davidkarlsen/flyway-operator/chart-release.yaml?branch=main&label=chart%20release)](https://github.com/davidkarlsen/flyway-operator/actions/workflows/chart-release.yaml)
[![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/davidkarlsen/flyway-operator)](go.mod)
[![e2e on kind](https://img.shields.io/github/actions/workflow/status/davidkarlsen/flyway-operator/build.yaml?branch=main&label=e2e%20on%20kind&logo=kubernetes)](https://github.com/davidkarlsen/flyway-operator/actions/workflows/build.yaml)
[![dependabot](https://img.shields.io/badge/dependabot-enabled-025e8c?logo=dependabot)](.github/dependabot.yml)

**Releases and distribution**
[![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/davidkarlsen/flyway-operator?sort=semver&filter=v*)](https://github.com/davidkarlsen/flyway-operator/releases)
[![Helm chart](https://img.shields.io/badge/dynamic/json?url=https%3A%2F%2Fartifacthub.io%2Fapi%2Fv1%2Fpackages%2Fhelm%2Fflyway-operator%2Fflyway-operator&query=%24.version&label=helm%20chart&logo=helm)](https://artifacthub.io/packages/helm/flyway-operator/flyway-operator)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/flyway-operator)](https://artifacthub.io/packages/search?repo=flyway-operator)
[![CRD Docs](https://img.shields.io/badge/CRD-Docs-brightgreen)](https://doc.crds.dev/github.com/davidkarlsen/flyway-operator)
[![downloads](https://img.shields.io/github/downloads/davidkarlsen/flyway-operator/total)](https://github.com/davidkarlsen/flyway-operator/releases)
[![release date](https://img.shields.io/github/release-date/davidkarlsen/flyway-operator?filter=v*)](https://github.com/davidkarlsen/flyway-operator/releases)
[![OLM](https://img.shields.io/badge/operator-OLM-ee0000?logo=redhat)](docs/installing.md)

**Platform and security**
[![Kubernetes](https://img.shields.io/badge/k8s-%E2%89%A51.35-326ce5?logo=kubernetes&logoColor=white)](docs/installing.md#requirements)
[![OpenShift](https://img.shields.io/badge/OpenShift-%E2%89%A54.22-EE0000?logo=redhatopenshift&logoColor=white)](docs/installing.md#requirements)
[![signed with cosign](https://img.shields.io/badge/signed-cosign-blue?logo=sigstore)](docs/verifying.md)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/davidkarlsen/flyway-operator/badge)](https://scorecard.dev/viewer/?uri=github.com/davidkarlsen/flyway-operator)
[![SLSA provenance](https://img.shields.io/badge/SLSA-provenance-green?logo=github)](docs/verifying.md)
[![Flyway 13](https://img.shields.io/badge/Flyway-13-CC0200?logo=flyway&logoColor=white)](docs/usage.md)
[![License](https://img.shields.io/github/license/davidkarlsen/flyway-operator)](LICENSE)

**Integrations**
[![Prometheus metrics](https://img.shields.io/badge/Prometheus-metrics-E6522C?logo=prometheus&logoColor=white)](docs/metrics.md)
[![Grafana dashboards](https://img.shields.io/badge/Grafana-dashboards-F46800?logo=grafana&logoColor=white)](docs/dashboards.md)
[![Argo CD health check](https://img.shields.io/badge/Argo%20CD-health%20check-EF7B4D?logo=argo&logoColor=white)](docs/argocd.md)

**Community**
[![GitHub stars](https://img.shields.io/github/stars/davidkarlsen/flyway-operator?style=social)](https://github.com/davidkarlsen/flyway-operator)
[![contributors](https://img.shields.io/github/contributors/davidkarlsen/flyway-operator)](https://github.com/davidkarlsen/flyway-operator/graphs/contributors)
[![commit activity](https://img.shields.io/github/commit-activity/m/davidkarlsen/flyway-operator)](https://github.com/davidkarlsen/flyway-operator/commits/main)
[![last commit](https://img.shields.io/github/last-commit/davidkarlsen/flyway-operator)](https://github.com/davidkarlsen/flyway-operator/commits/main)
[![open issues](https://img.shields.io/github/issues/davidkarlsen/flyway-operator)](https://github.com/davidkarlsen/flyway-operator/issues)
[![open pull requests](https://img.shields.io/github/issues-pr/davidkarlsen/flyway-operator)](https://github.com/davidkarlsen/flyway-operator/pulls)

## Why

Running database migrations from your application's startup, from CI scripts or by hand is easy to get wrong:
two replicas race, a deploy goes out before the schema is ready, or nobody can tell which version was applied.
With the Flyway operator the migration is part of your deployment, like any other Kubernetes resource:

- **Declarative.** A `Migration` says which database to migrate and which SQL to apply. Commit it next to your
  application and let GitOps roll it out.
- **Runs once per change.** A new SQL image or a changed setting runs Flyway again; an unchanged `Migration` does not.
- **Clear status.** `Applied`, `Progressing` and `Failed` conditions show the outcome, including which SQL image was
  applied last, in `kubectl`, Argo CD or your dashboards.
- **Safe defaults.** Failed attempts are retried a limited number of times, each attempt has a time limit, and the
  migration runs in a locked-down pod that passes Pod Security "restricted" and the OpenShift restricted SCC.
- **Your SQL, your image.** Ship your SQL files in a small container image, versioned and released with your
  application. Flyway reads them straight from the image.
- **Observable.** Prometheus metrics, copy-paste alert rules and Grafana dashboards, split between the platform team that
  runs the operator and the teams that own the migrations.
- **Fits your delivery.** Argo CD can wait for the migration before it rolls out your application.

## How it looks

```yaml
apiVersion: flyway.davidkarlsen.com/v1alpha1
kind: Migration
metadata:
  name: my-app
  namespace: my-app
spec:
  database:
    jdbcUrl: "jdbc:postgresql://my-db:5432/my_app"
    username: myuser
    credentials:
      name: my-app-db       # Secret holding the password
      key: password
  migrationSource:
    imageRef: registry.example.com/my-app-sql:1.4.0   # your SQL files, under /sql
```

```shell
$ kubectl wait migration/my-app --for=condition=Applied
migration.flyway.davidkarlsen.com/my-app condition met
```

Every option is described in the [usage guide](docs/usage.md).

## Get started

1. **Install** the operator with OLM on OpenShift, with Helm, or with kustomize: [installing](docs/installing.md).
   It needs Kubernetes 1.35 or later (OpenShift 4.22 or later).
2. **Package your SQL** in a container image and **create a `Migration`**: [usage](docs/usage.md).
3. **Watch it**: [metrics and alerts](docs/metrics.md), [Grafana dashboards](docs/dashboards.md), and
   [Argo CD](docs/argocd.md) to gate your rollout on the migration.

Releases are signed and come with build provenance: [verifying releases](docs/verifying.md).

## Documentation

| | |
|---|---|
| [Installing](docs/installing.md) | OLM, Helm and kustomize, requirements, upgrade notes |
| [Usage](docs/usage.md) | The `Migration` resource, status, retries, time limits, security context, labels |
| [Metrics and alerts](docs/metrics.md) | Prometheus metrics and copy-paste alert rules |
| [Dashboards](docs/dashboards.md) | Grafana dashboards for the operator and for migrations |
| [Argo CD](docs/argocd.md) | Make Argo CD wait for a migration |
| [Verifying releases](docs/verifying.md) | Check signatures and provenance |
| [API reference](https://doc.crds.dev/github.com/davidkarlsen/flyway-operator) | Generated CRD documentation |

## Support and contributing

Questions, bugs and ideas are welcome as [issues](https://github.com/davidkarlsen/flyway-operator/issues); planned
work is in the [backlog](https://github.com/davidkarlsen/flyway-operator/issues?q=is%3Aissue+is%3Aopen+label%3Aenhancement).
Report security issues privately, see the [security policy](SECURITY.md). To build, test or change the operator,
see the [contribution guide](CONTRIBUTING.md).

Licensed under the [Apache License 2.0](LICENSE).

## Star history

[![Star History Chart](https://api.star-history.com/svg?repos=davidkarlsen/flyway-operator&type=Date)](https://star-history.com/#davidkarlsen/flyway-operator&Date)
