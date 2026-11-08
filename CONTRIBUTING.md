# Contributing

## Issues

Include as much as you can: operator and Kubernetes versions, the `Migration` resource, its status
(`kubectl get migration <name> -o yaml`), and the operator and Flyway Job logs.

## Prerequisites

- Go, at the version in `go.mod`
- Docker
- For the e2e tests: [kind](https://kind.sigs.k8s.io/) and `kubectl`

`controller-gen`, `kustomize`, `setup-envtest`, `operator-sdk` and `opm` are downloaded into `./bin/` by the
Makefile on first use. `make help` lists all targets.

## Before opening a pull request

Run the same checks as CI:

```sh
make test                                 # regenerates CRDs/RBAC/deepcopy, then fmt, vet and the envtest suite
golangci-lint run --build-tags e2e ./...  # CI runs the latest golangci-lint with the default config
```

`make test` runs the unit tests and the envtest specs against a real Kubernetes API server (no cluster needed).
Commit any files it regenerates.

Optionally, run the end-to-end tests locally. CI runs them on every pull request that changes code:

```sh
make test-e2e           # kind cluster + local registry, real Flyway Jobs against Postgres
make cleanup-test-e2e   # delete the kind cluster and registry
```

Add or update tests with every change.

## Changing the API

When you change `api/v1alpha1/`:

1. Use kubebuilder markers for validation and defaults (`+kubebuilder:validation:...`, `+kubebuilder:default`).
2. Run `make manifests generate`.
3. Update the examples in [`docs/usage.md`](docs/usage.md).
4. `docs/usage.md` has exactly one reference example with the same `metadata.name` as the OLM sample, plus as many per-use-case examples with their own names as you like. Keep `config/samples/` a minimal, valid subset of the reference. It only feeds the OLM `alm-examples`
   (the pre-filled "Create Migration" form); it is not documentation.

## Documentation

User documentation lives in [`docs/`](docs/README.md). Put examples there, not in YAML files elsewhere in the repo.

## CI

- `build` runs lint, `make test` and the container build.
- `e2e` runs the kind tests.
- `ci-success` is the only required check. It passes when `build` and `e2e` succeeded, or were skipped because
  the change only touches docs (`*.md`, `docs/`), the Helm chart or similar files.
- Changes to `config/helm-chart/` are tested by the chart-test workflow.

## Running the operator against a cluster

```sh
make install run                                     # install the CRD and run the controller from your machine
make docker-build deploy IMG=<registry>/flyway-operator:<tag>   # build and deploy in-cluster
make docker-build IMG=<registry>/flyway-operator:<tag> PLATFORM=linux/amd64   # x86 cluster from an Apple Silicon Mac
```

`docker-build` targets the host platform unless `PLATFORM` is set. The Dockerfile builds on the host platform and cross-compiles the Go binary, so building `linux/amd64` on Apple Silicon does not run the compiler under emulation.

## Releasing (maintainers)

1. Push a `vX.Y.Z` tag on `main`. CI builds and pushes the operator image and the OLM bundle and catalog,
   signs and attests them, and creates the GitHub release (see [Verifying releases](docs/verifying.md)).
   CI does not commit anything back to `main`: the catalog is built from the committed `catalog-template.yaml`
   plus the bundles of all later `v*` tags up to the new one, so `catalog-template.yaml` does not need updating
   per release.
2. Open a pull request that updates the Helm chart: `image.tag` and `appVersion` to the new release, a new chart
   `version`, and the chart's copies of the CRD (`make helm-crds`), the Grafana dashboards (`make helm-dashboards`)
   and the alert rules (`make helm-rules`), plus `values.schema.json` (`make helm-schema`) when `values.yaml`
   changed. Chart CI fails if any of them is stale. The chart is published when that pull request merges.

More background: the [Kubebuilder book](https://book.kubebuilder.io/introduction.html).
