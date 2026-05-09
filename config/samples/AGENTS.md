<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# samples

## Purpose
Sample `Migration` custom resources used in docs, demos, and as the source of truth for the OLM CSV `alm-examples` annotation.

## Key Files

| File | Description |
|------|-------------|
| `flyway_v1alpha1_migration.yaml` | Minimal but realistic Migration CR — referenced from `USING.md` |
| `kustomization.yaml` | Kustomize wiring (used by OLM bundling to populate `alm-examples`) |

## For AI Agents

### Working In This Directory
- After adding/renaming spec fields in `api/v1alpha1/migration_types.go`, update this sample so the README/USING examples stay accurate and `make bundle` regenerates the CSV's `alm-examples` correctly.

<!-- MANUAL: -->
