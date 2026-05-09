<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# api

## Purpose
Container directory for versioned API packages. Per Kubebuilder layout, each subdirectory is a `<version>` package within the `flyway.davidkarlsen.com` group.

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `v1alpha1/` | `Migration` CRD Go types and scheme registration (see `v1alpha1/AGENTS.md`) |

## For AI Agents

### Working In This Directory
- Do not add code at this level; new content belongs inside a versioned package.
- A new API version means a new sibling directory (`v1beta1/`, etc.) plus an entry in `PROJECT` under `resources:`.

<!-- MANUAL: -->
