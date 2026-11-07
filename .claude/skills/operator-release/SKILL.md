---
name: operator-release
description: Release flyway-operator - check main is ready, the maintainer pushes the vX.Y.Z tag, watch and verify the tag build (image, OLM bundle and catalog, signatures, provenance, GitHub release), then prepare the Helm chart release PR. Use when asked to "release", "cut vX.Y.Z", "prepare the chart PR" or to check a release.
---

# release

The maintainer pushes tags; this skill prepares, watches and verifies. Don't push tags or merge the chart PR without being told to.

## 1. Ready check

- Everything planned for the release is merged; drafts that wait for this release stay drafts.
- `main` CI is green on the latest code-changing commit up to the one to be tagged (docs-only commits skip them), including `e2e` and `govulncheck` (`gh run list --branch main -L 3`, `gh run view <id>`). The tag's `release` job only waits for `build`, so these must be green beforehand.
- `make manifests generate` leaves no diff on `main`.
- Note the previous tag and summarise what's in the release (merged PRs since it). Call out behaviour changes and breaking changes (defaults, minimum Kubernetes, renamed labels/RBAC, Helm CRD step) for the release notes.

## 2. Tag build (after the maintainer pushes `vX.Y.Z`)

`build.yaml` runs `build` (image via `make docker-buildx`, bundle artifact) and then the tag-only `release` job: catalog (template from the committed `catalog-template.yaml` + later `v*` tags that have a bundle image, nothing pushed to `main`), cosign signing, provenance attestations, release assets, `gh release create`.

Watch it: `gh run watch` / `gh run view --log-failed`. The `release` job is re-runnable: on a transient failure, "Re-run failed jobs".

Verify afterwards by running **every block in `docs/verifying.md`** for the new tag: `cosign verify` for the operator image; `gh attestation verify` for the operator, bundle and catalog images; `cosign verify-blob` on `checksums.txt` plus `sha256sum -c` and the asset attestation. Then:

```sh
gh release view v<version>      # assets: bundle tarball, images list, checksums + .sigstore.json, .intoto.jsonl
opm render ghcr.io/davidkarlsen/flyway-operator-catalog:v<version> | grep -c 'flyway-operator.v<version>'   # new bundle is in the catalog
```

`:alpha` must have moved to the new catalog only if this is the highest release tag.

## 3. Helm chart release PR

A separate PR, merged after the tag build succeeded (merging any `config/helm-chart/` change publishes the chart immediately):

- `make helm-crds` (sync the chart CRD; chart-test fails if it's stale). The target and the chart-test gate come from #498.
- `values.yaml` `image.tag` and `Chart.yaml` `appVersion` to the new version; bump the chart `version` (minor if anything user-visible changed).
- `kubeVersion` in `Chart.yaml` if the minimum Kubernetes changed.
- Fold in any draft chart-fix PRs that were waiting for this release.
- Release note: Helm never upgrades `crds/`, so users must `kubectl apply` the CRD before `helm upgrade`.

Review it with the `review-loop` skill. **Merge it only when the user says so**; merging publishes the chart.

After it merges, verify the chart release the same way: the `<chart>.tgz.sigstore.json` and `.intoto.jsonl` assets on the `flyway-operator-<chart version>` release, checked with the chart commands in `docs/verifying.md` (identity `chart-release.yaml`).

## 4. Gate

After each release the maintainer upgrades their own clusters first. Don't merge anything that targets the next release (e.g. a draft that raises the minimum Kubernetes) until the maintainer confirms.
