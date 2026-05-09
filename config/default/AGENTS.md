<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# default

## Purpose
The default kustomize overlay rendered by `make deploy`. Composes the CRD, manager Deployment, RBAC, and metrics service into a single applyable bundle, applying the `IMG` substitution.

## Key Files

| File | Description |
|------|-------------|
| `kustomization.yaml` | Top-level overlay — sets the namespace, name prefix, image override, and resources list |
| `manager_config_patch.yaml` | Patches the manager container args/env (e.g. metrics bind address) |
| `metrics_service.yaml` | `Service` exposing the controller's metrics port |

## For AI Agents

### Working In This Directory
- The `IMG` env var passed to `make deploy` is substituted via `kustomize edit set image` against this overlay.
- Adding a new manifest to the deployed bundle? Reference it from `kustomization.yaml`'s `resources:` list.

<!-- MANUAL: -->
