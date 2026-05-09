<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# manager

## Purpose
Manifests describing the operator Deployment (and its namespace) installed by the `default` overlay.

## Key Files

| File | Description |
|------|-------------|
| `manager.yaml` | The operator `Deployment` (and Namespace). Image is `controller:latest`, replaced by kustomize at deploy time |
| `kustomization.yaml` | Kustomize wiring |

## For AI Agents

### Working In This Directory
- Resource limits, probes, security context, and arg flags live here. Mirror any change that affects user-visible knobs into `config/helm-chart/flyway-operator/templates/deployment.yaml`.

<!-- MANUAL: -->
