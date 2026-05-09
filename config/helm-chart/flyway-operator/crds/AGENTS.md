<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# crds

## Purpose
CRD copy installed by Helm during pre-install. Helm treats files in `crds/` specially: installed once, never upgraded.

## Key Files

| File | Description |
|------|-------------|
| `flyway.davidkarlsen.com_migrations.yaml` | Mirror of `config/crd/bases/...`. Refresh whenever the API changes. |

## For AI Agents

### Working In This Directory
- Don't hand-edit; copy from `config/crd/bases/` after `make manifests`.
- Breaking changes here cannot be applied via `helm upgrade` — they need manual `kubectl apply`. Document in the chart README when bumping.

<!-- MANUAL: -->
