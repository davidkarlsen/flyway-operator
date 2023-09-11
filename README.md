# flyway-operator
[Kubernetes-operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) to apply [Flyway migrations](https://flywaydb.org/).


## Badges
![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/davidkarlsen/flyway-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/davidkarlsen/flyway-operator)](https://goreportcard.com/report/github.com/davidkarlsen/flyway-operator)
![build](https://github.com/davidkarlsen/flyway-operator/workflows/build/badge.svg?branch=main)
[![codecov](https://codecov.io/gh/davidkarlsen/flyway-operator/branch/main/graph/badge.svg)](https://codecov.io/gh/davidkarlsen/flyway-operator)
![GitHub release (latest SemVer)](https://img.shields.io/github/v/release/davidkarlsen/flyway-operator?sort=semver)
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
1. Install Instances of Custom Resources:

```sh
kubectl apply -f config/samples/
```

2. Build and push your image to the location specified by `IMG`:

```sh
make docker-build docker-push IMG=<some-registry>/flyway-operator:tag
```

3. Deploy the controller to the cluster with the image specified by `IMG`:

```sh
make deploy IMG=<some-registry>/flyway-operator:tag
```

### Uninstall CRDs
To delete the CRDs from the cluster:

```sh
make uninstall
```

### Undeploy controller
UnDeploy the controller from the cluster:

```sh
make undeploy
```

## Contributing
* Create issues - add as much info, such as logs, versions and CR-definitions to the issue
* Create PRs including tests
  * Make sure you run `make manifests fmt vet` prior to opening the PR

### How it works
This project aims to follow the Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the cluster.

### Test It Out
1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

