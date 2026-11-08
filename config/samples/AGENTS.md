# samples

**Parent context:** `../AGENTS.md`
**Generated:** 2026-05-09 · **Updated:** 2026-11-07

## Purpose
A minimal `Migration` used only as the source of the OLM CSV `alm-examples` annotation (the pre-filled "Create Migration" form in OperatorHub / the OpenShift console). Documented examples live in `docs/usage.md` and `docs/examples/`.

## Key Files

| File | Description |
|------|-------------|
| `flyway_v1alpha1_migration.yaml` | Minimal, valid Migration CR for `alm-examples` — not documentation |
| `kustomization.yaml` | Kustomize wiring (used by OLM bundling to populate `alm-examples`) |

## For AI Agents

### Working In This Directory
- After adding/renaming spec fields in `api/v1alpha1/migration_types.go`, update `docs/usage.md`. Only touch this sample if it would no longer validate against the CRD; keep it minimal. `internal/controller/docs_examples_test.go` requires `docs/usage.md` to contain exactly one reference `Migration` with the same `metadata.name` as this sample (other, differently named per-use-case examples are allowed) and this sample to be a subset of that reference (same `apiVersion` and name, every `spec` value equal), and applies both to the CRD.

## Manual Notes

Notes under this heading are written by humans and preserved on regeneration.
