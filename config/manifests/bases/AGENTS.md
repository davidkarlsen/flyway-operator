# bases

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
Seed `ClusterServiceVersion` consumed by `make bundle`. Contains the human-authored CSV metadata (display name, maintainers, description, links, icon) — install strategy and CRD owned-list are merged in by `operator-sdk generate bundle` from `config/default/` and `config/crd/bases/`.

## Key Files

| File | Description |
|------|-------------|
| `flyway-operator.clusterserviceversion.yaml` | CSV seed; edit when changing operator description, maintainers, keywords, links, icon or `minKubeVersion` (1.35.0, as image volumes need Kubernetes 1.35). It also carries the community-catalog metadata: `features.operators.openshift.io/*` (all `"false"`, matching what the operator does), the `operatorframework.io/arch.*` / `os.linux` labels (amd64 and arm64, the architectures the default Flyway image `docker.io/flyway/flyway` supports, since Migration Jobs run it; the operator image itself is built for more in `PLATFORMS`), `repository`, `support` (the issues URL) and the short `description` annotation (at most 135 characters). `containerImage` is a placeholder that `make bundle` replaces with `IMG` |

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
