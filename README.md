# flyway-operator
[Kubernetes-operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) to apply [Flyway migrations](https://flywaydb.org/).


## Badges
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/davidkarlsen/flyway-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/davidkarlsen/flyway-operator)](https://goreportcard.com/report/github.com/davidkarlsen/flyway-operator)
![build](https://github.com/davidkarlsen/flyway-operator/workflows/build/badge.svg?branch=main)
[![codecov](https://codecov.io/gh/davidkarlsen/flyway-operator/branch/main/graph/badge.svg)](https://codecov.io/gh/davidkarlsen/flyway-operator)
![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/davidkarlsen/flyway-operator?sort=semver)
[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/flyway-operator)](https://artifacthub.io/packages/search?repo=flyway-operator)
[![CRD Docs](https://img.shields.io/badge/CRD-Docs-brightgreen)](https://doc.crds.dev/github.com/davidkarlsen/flyway-operator)
[![Stargazers over time](https://starchart.cc/davidkarlsen/flyway-operator.svg)](https://starchart.cc/davidkarlsen/flyway-operator)


## Description
The operator will spawn [Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/) using the
[flyway-docker image](https://hub.docker.com/r/flyway/flyway).

See `config/samples` for example CRs.

It is still an early project and I want to further develop some day-2 elements as described in the [backlog](https://github.com/davidkarlsen/flyway-operator/issues?q=is%3Aissue+is%3Aopen+label%3Aenhancement).

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running on the cluster

See [INSTALLING.md](INSTALLING.md)


## Contributing

See the [contribution guide](CONTRIBUTING.md)
