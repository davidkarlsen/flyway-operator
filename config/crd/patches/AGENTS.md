<!-- Parent: ../AGENTS.md -->
<!-- Generated: 2026-05-09 | Updated: 2026-05-09 -->

# patches

## Purpose
Kustomize patches that conditionally enable conversion-webhook plumbing on the CRD. Off by default; uncomment in `config/crd/kustomization.yaml` when introducing a new API version.

## Key Files

| File | Description |
|------|-------------|
| `webhook_in_migrations.yaml` | Patch that adds the `webhook` conversion strategy block to the CRD |
| `cainjection_in_migrations.yaml` | Patch that adds the `cert-manager.io/inject-ca-from` annotation |

<!-- MANUAL: -->
