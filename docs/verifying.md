# Verifying release artifacts

Every `vX.Y.Z` release is signed with [Sigstore](https://www.sigstore.dev/) keyless signing (GitHub OIDC) and
carries [SLSA build provenance](https://slsa.dev/) from GitHub artifact attestations. No keys are involved: the
signing identity is the `build.yaml` workflow running on the release tag.

| Artifact | Signature | Provenance |
|----------|-----------|------------|
| `ghcr.io/davidkarlsen/flyway-operator:<version>` (index and every platform image) | cosign, in the registry | attestation of the index, in the registry |
| `ghcr.io/davidkarlsen/flyway-operator-bundle:v<version>` | cosign, in the registry | attestation, in the registry |
| `ghcr.io/davidkarlsen/flyway-operator-catalog:v<version>` | cosign, in the registry | attestation, in the registry |
| `checksums.txt` release asset | `checksums.txt.sigstore.json` | - |
| `flyway-operator-bundle-<tag>.tar.gz` and `images-<tag>.txt` release assets (listed in `checksums.txt`) | via `checksums.txt` | `flyway-operator-<tag>.intoto.jsonl` |
| Helm chart releases (`flyway-operator-<chart version>.tgz`) | `<chart>.tgz.sigstore.json`, made by `chart-release.yaml` | `flyway-operator-<chart version>.intoto.jsonl` |

`:alpha` points at the catalog of the newest release, so it has the same signature and attestation as that release's `-catalog:v<version>`.

Releases made before signing was added are not signed.

Requires [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) v3 or later (releases are signed
with cosign v3) and the [GitHub CLI](https://cli.github.com/).

## Container images

```sh
TAG=v0.5.0          # git tag / GitHub release
VERSION=${TAG#v}    # operator image tag
cosign verify ghcr.io/davidkarlsen/flyway-operator:${VERSION} \
  --certificate-identity-regexp '^https://github\.com/davidkarlsen/flyway-operator/\.github/workflows/build\.yaml@refs/tags/v.*$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

gh attestation verify oci://ghcr.io/davidkarlsen/flyway-operator:${VERSION} -R davidkarlsen/flyway-operator
```

The operator image is multi-platform. `cosign verify` also works on a single platform image
(`ghcr.io/davidkarlsen/flyway-operator@sha256:...`), because every platform manifest is signed.
The provenance attestation is attached to the index digest, so run `gh attestation verify` against the
tag or the index digest, not a platform digest.

The bundle and catalog images use the `v`-prefixed tag:

```sh
gh attestation verify oci://ghcr.io/davidkarlsen/flyway-operator-bundle:${TAG} -R davidkarlsen/flyway-operator
gh attestation verify oci://ghcr.io/davidkarlsen/flyway-operator-catalog:${TAG} -R davidkarlsen/flyway-operator
```

## Release assets

```sh
TAG=v0.5.0
gh release download ${TAG} -R davidkarlsen/flyway-operator -D flyway-operator-${TAG}
cd flyway-operator-${TAG}

cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/davidkarlsen/flyway-operator/\.github/workflows/build\.yaml@refs/tags/v.*$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
sha256sum -c checksums.txt

gh attestation verify flyway-operator-bundle-${TAG}.tar.gz -R davidkarlsen/flyway-operator
```

`images-<tag>.txt` lists the image references and digests published by the release.

## Helm chart

Chart releases are signed by `chart-release.yaml` on `main`:

```sh
CHART=flyway-operator-X.Y.Z   # GitHub release name of the chart (chart name and version)
gh release download ${CHART} -R davidkarlsen/flyway-operator -D ${CHART}
cd ${CHART}

cosign verify-blob ${CHART}.tgz --bundle ${CHART}.tgz.sigstore.json \
  --certificate-identity-regexp '^https://github\.com/davidkarlsen/flyway-operator/\.github/workflows/chart-release\.yaml@refs/(heads/main|tags/chart-.*)$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com

gh attestation verify ${CHART}.tgz -R davidkarlsen/flyway-operator
```
