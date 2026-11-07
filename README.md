# flyway-operator
[Kubernetes-operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) to apply [Flyway migrations](https://flywaydb.org/).


## Badges
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/davidkarlsen/flyway-operator)
[![build](https://github.com/davidkarlsen/flyway-operator/actions/workflows/build.yaml/badge.svg?branch=main)](https://github.com/davidkarlsen/flyway-operator/actions/workflows/build.yaml)
[![CodeQL](https://github.com/davidkarlsen/flyway-operator/actions/workflows/codeql.yaml/badge.svg?branch=main)](https://github.com/davidkarlsen/flyway-operator/actions/workflows/codeql.yaml)
[![codecov](https://codecov.io/gh/davidkarlsen/flyway-operator/branch/main/graph/badge.svg)](https://codecov.io/gh/davidkarlsen/flyway-operator)
![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/davidkarlsen/flyway-operator?sort=semver)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/flyway-operator)](https://artifacthub.io/packages/search?repo=flyway-operator)
[![CRD Docs](https://img.shields.io/badge/CRD-Docs-brightgreen)](https://doc.crds.dev/github.com/davidkarlsen/flyway-operator)
[![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/davidkarlsen/flyway-operator/badge)](https://scorecard.dev/viewer/?uri=github.com/davidkarlsen/flyway-operator)
[![License](https://img.shields.io/github/license/davidkarlsen/flyway-operator)](https://github.com/davidkarlsen/flyway-operator/blob/main/LICENSE)
[![GitHub stars](https://img.shields.io/github/stars/davidkarlsen/flyway-operator?style=social)](https://github.com/davidkarlsen/flyway-operator)


## Description
The operator will spawn [Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/) using the
[flyway-docker image](https://hub.docker.com/r/flyway/flyway).

See the [documentation](docs/README.md) for features, installation and examples.

It is still an early project and I want to further develop some day-2 elements as described in the [backlog](https://github.com/davidkarlsen/flyway-operator/issues?q=is%3Aissue+is%3Aopen+label%3Aenhancement).

## Argo CD
To make Argo CD wait for a `Migration` to apply before it syncs your application, see [docs/argocd.md](docs/argocd.md).

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster

See [docs/installing.md](docs/installing.md)


## Contributing

See the [contribution guide](CONTRIBUTING.md)

## Star history

[![Star history of davidkarlsen/flyway-operator](https://next.ossinsight.io/widgets/official/analyze-repo-stars-history/thumbnail.png?repo_id=688360587&image_size=auto&color_scheme=light)](https://ossinsight.io/analyze/davidkarlsen/flyway-operator)
