# helm-chart

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
Container for the published Helm chart distribution.

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `flyway-operator/` | Chart source (see `flyway-operator/AGENTS.md`) |

## Key Files

| File | Description |
|------|-------------|
| `test-values.sh` | `helm template` cases that must pass or fail `values.schema.json`, run by chart-test. Add a case when a value's type or shape changes |

## For AI Agents

### Working In This Directory
- Chart is published to `gh-pages` and consumed via `helm repo add flyway-operator https://davidkarlsen.github.io/flyway-operator`. The `chart-release.yaml` workflow (chart-releaser) handles packaging, publishing, signing and provenance.

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
