<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# manifests

## Purpose
Inputs for `operator-sdk generate bundle` (`make bundle`). Hosts the seed `ClusterServiceVersion` whose values are merged with `config/default/` to produce `bundle/manifests/`.

## Key Files

| File | Description |
|------|-------------|
| `kustomization.yaml` | Wires this base into the bundle generator |

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `bases/` | Seed CSV containing maintainer/description/links/icon (see `bases/AGENTS.md`) |

<!-- MANUAL: -->
