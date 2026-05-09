<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# bases

## Purpose
Seed `ClusterServiceVersion` consumed by `make bundle`. Contains the human-authored CSV metadata (display name, maintainers, description, links, icon) — install strategy and CRD owned-list are merged in by `operator-sdk generate bundle` from `config/default/` and `config/crd/bases/`.

## Key Files

| File | Description |
|------|-------------|
| `flyway-operator.clusterserviceversion.yaml` | CSV seed; edit when changing operator description, maintainers, keywords, links, or icon |

<!-- MANUAL: -->
