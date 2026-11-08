# config

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
Kustomize bases and overlays consumed by `make install`, `make deploy`, `make manifests`, and `make bundle`. Also hosts the Helm chart published to `gh-pages`.

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `crd/` | Generated `Migration` CRD + optional conversion-webhook patches (see `crd/AGENTS.md`) |
| `default/` | Default deployment overlay used by `make deploy` (see `default/AGENTS.md`) |
| `manager/` | Operator Deployment + namespace manifests (see `manager/AGENTS.md`) |
| `rbac/` | ClusterRole/Role bindings for the manager and the metrics endpoint (see `rbac/AGENTS.md`) |
| `samples/` | Minimal Migration used only as the OLM CSV `alm-examples` source (see `samples/AGENTS.md`); documented examples live in `docs/` |
| `prometheus/` | `ServiceMonitor` and alert `PrometheusRule` for Prometheus Operator, included by `default/` (see `prometheus/AGENTS.md`) |
| `grafana/` | Grafana dashboard JSON and the `flyway-operator-dashboards` ConfigMap generator, included by `default/` (see `grafana/AGENTS.md`) |
| `manifests/` | OLM `ClusterServiceVersion` base (including `minKubeVersion`), used by `make bundle` (see `manifests/AGENTS.md`) |
| `scorecard/` | Operator-SDK scorecard test config (see `scorecard/AGENTS.md`) |
| `helm-chart/` | Helm chart source for `flyway-operator/flyway-operator` (see `helm-chart/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Most files are generated or kustomize-managed. After changing API types or RBAC markers, run `make manifests` rather than hand-editing CRDs/Roles.
- Use `kustomize` (vendored to `./bin/kustomize` by `make kustomize`) to render — do not assume `kubectl apply -k` works identically across kustomize versions.
- Helm chart in `helm-chart/` is a separate distribution path; updates to RBAC/Deployment in `config/` should be mirrored there if the chart exposes the same surface; `make helm-crds` copies the generated CRD into the chart. After changing the chart's `values.yaml`, run `make helm-schema` to regenerate `values.schema.json` (types and enums come from `# @schema` annotations; chart CI fails if it is stale).

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
