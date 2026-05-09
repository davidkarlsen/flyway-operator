<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# scorecard

## Purpose
Operator-SDK scorecard test configuration. Built into the bundle via `make bundle` and run by `operator-sdk scorecard` against an installed bundle.

## Key Files

| File | Description |
|------|-------------|
| `kustomization.yaml` | Kustomize wiring |

## Subdirectories

| Directory | Purpose |
|-----------|---------|
| `bases/` | Base scorecard config (see `bases/AGENTS.md`) |
| `patches/` | Test selectors: `basic` (CR validation) and `olm` (OLM-specific) suites (see `patches/AGENTS.md`) |

<!-- MANUAL: -->
