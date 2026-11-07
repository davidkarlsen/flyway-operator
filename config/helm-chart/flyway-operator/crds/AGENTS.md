# crds

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
CRD copy installed by Helm during pre-install. Helm treats files in `crds/` specially: installed once, never upgraded.

## Key Files

| File | Description |
|------|-------------|
| `flyway.davidkarlsen.com_migrations.yaml` | Mirror of `config/crd/bases/...`. Refresh whenever the API changes. |

## For AI Agents

### Working In This Directory
- Don't hand-edit; run `make helm-crds` (it regenerates and copies from `config/crd/bases/`). chart-test fails if the copy is stale.
- Breaking changes here cannot be applied via `helm upgrade` — they need manual `kubectl apply`. Document in the chart README when bumping.

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
