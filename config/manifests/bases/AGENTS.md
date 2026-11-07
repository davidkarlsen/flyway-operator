# bases

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
Seed `ClusterServiceVersion` consumed by `make bundle`. Contains the human-authored CSV metadata (display name, maintainers, description, links, icon) — install strategy and CRD owned-list are merged in by `operator-sdk generate bundle` from `config/default/` and `config/crd/bases/`.

## Key Files

| File | Description |
|------|-------------|
| `flyway-operator.clusterserviceversion.yaml` | CSV seed; edit when changing operator description, maintainers, keywords, links, icon or `minKubeVersion` (1.35.0, as image volumes need Kubernetes 1.35) |

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
