#!/usr/bin/env bash
# Submit a flyway-operator release to the OpenShift Community Operators catalog
# (redhat-openshift-ecosystem/community-operators-prod): add operators/flyway-operator/<version>/ from the
# release's bundle tarball plus a release-config.yaml, and open a PR from the fork. The upstream pipeline then
# builds the bundle and, after merge, updates the semver template and the catalogs itself.
#
# Usage: hack/community-release.sh vX.Y.Z
# Environment:
#   DRY_RUN=1            stop before anything is pushed or any fork/PR call is made; needs no token
#   GH_TOKEN             read access to the flyway-operator release (gh release download)
#   COMMUNITY_TOKEN      token with push rights on the fork and PR rights upstream; required unless DRY_RUN=1
#   WORKDIR              scratch directory, must not exist or be empty (default: a new mktemp dir)
#   RELEASE_REPO / COMMUNITY_UPSTREAM / COMMUNITY_FORK   repositories (defaults below)
set -euo pipefail

TAG="${1:?usage: $0 vX.Y.Z}"
[[ "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "not a release tag (pre-releases are not submitted): ${TAG}" >&2; exit 1; }
VERSION="${TAG#v}"

RELEASE_REPO="${RELEASE_REPO:-davidkarlsen/flyway-operator}"
UPSTREAM="${COMMUNITY_UPSTREAM:-redhat-openshift-ecosystem/community-operators-prod}"
FORK="${COMMUNITY_FORK:-davidkarlsen/community-operators-prod}"
FORK_OWNER="${FORK%%/*}"
OPERATOR=flyway-operator
OPDIR="operators/${OPERATOR}"
BRANCH="${OPERATOR}-${VERSION}"
SUBJECT="operator ${OPERATOR} (${VERSION})"
DRY_RUN="${DRY_RUN:-0}"

if [ "${DRY_RUN}" != 1 ] && [ -z "${COMMUNITY_TOKEN:-}" ]; then
  echo "COMMUNITY_TOKEN is not set (use DRY_RUN=1 to run without it)" >&2; exit 1
fi

WORKDIR="${WORKDIR:-$(mktemp -d)}"
mkdir -p "${WORKDIR}"
[ -z "$(ls -A "${WORKDIR}")" ] || { echo "WORKDIR ${WORKDIR} is not empty" >&2; exit 1; }
DL="${WORKDIR}/download"; BUNDLE="${WORKDIR}/bundle"; REPO="${WORKDIR}/community"
mkdir -p "${DL}" "${BUNDLE}"

# The token reaches git through the environment, never through argv, the URL or .git/config.
git_fork() {
  local auth
  auth="$(printf 'x-access-token:%s' "${COMMUNITY_TOKEN}" | base64 | tr -d '\n')"
  GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0="http.https://github.com/.extraheader" \
    GIT_CONFIG_VALUE_0="AUTHORIZATION: basic ${auth}" git -C "${REPO}" "$@"
}
gh_community() { GH_TOKEN="${COMMUNITY_TOKEN}" gh "$@"; }
sha256_check() { if command -v sha256sum >/dev/null; then sha256sum -c -; else shasum -a 256 -c -; fi; }

# 1. bundle tarball, verified against the release's checksums.txt
tarball="flyway-operator-bundle-${TAG}.tar.gz"
gh release download "${TAG}" -R "${RELEASE_REPO}" -D "${DL}" -p "${tarball}" -p checksums.txt
(cd "${DL}" && grep -F " ${tarball}" checksums.txt | sha256_check)
tar -xzf "${DL}/${tarball}" -C "${BUNDLE}" bundle/manifests bundle/metadata bundle/tests
for d in manifests metadata tests; do [ -d "${BUNDLE}/bundle/${d}" ] || { echo "bundle/${d} missing from ${tarball}" >&2; exit 1; }; done

# 2. upstream main (shallow, only this operator's directory)
git clone -q --depth 1 --filter=blob:none --sparse --branch main "https://github.com/${UPSTREAM}.git" "${REPO}"
git -C "${REPO}" sparse-checkout set "${OPDIR}"

# 7. idempotency: version already upstream, a PR for it, or its branch on the fork
if [ -d "${REPO}/${OPDIR}/${VERSION}" ]; then
  echo "skip: ${OPDIR}/${VERSION} already exists upstream"; exit 0
fi
if [ "${DRY_RUN}" != 1 ]; then
  prs="$(gh_community pr list --repo "${UPSTREAM}" --head "${FORK_OWNER}:${BRANCH}" --state all --json url --jq '.[].url')"
  if [ -n "${prs}" ]; then echo "skip: PR already exists: ${prs}"; exit 0; fi
  if git ls-remote --exit-code --heads "https://github.com/${FORK}.git" "${BRANCH}" >/dev/null 2>&1; then
    echo "skip: branch ${BRANCH} already exists on ${FORK}"; exit 0
  fi
  # keep the fork's main level with upstream main; the PR branch is based on upstream main either way
  gh_community repo sync "${FORK}" --branch main || echo "warning: could not sync ${FORK} main" >&2
fi

# 3-4. branch, version directory (manifests, metadata, tests unchanged) and release-config.yaml
git -C "${REPO}" checkout -q -b "${BRANCH}"
dest="${REPO}/${OPDIR}/${VERSION}"
mkdir -p "${dest}"
cp -R "${BUNDLE}/bundle/manifests" "${BUNDLE}/bundle/metadata" "${BUNDLE}/bundle/tests" "${dest}/"
# the semver template adds the bundle to the update graph; replaces/skips exist only for basic templates
cat > "${dest}/release-config.yaml" <<YAML
---
catalog_templates:
  - template_name: semver.yaml
    channels: [Stable]
    defaultChannel: Stable
YAML

# 5. DCO-signed commit as the owner, no trailers beyond Signed-off-by
git -C "${REPO}" add "${OPDIR}/${VERSION}"
git -C "${REPO}" -c user.name="David J. M. Karlsen" -c user.email="david@davidkarlsen.com" \
  commit -q -s -m "${SUBJECT}" -m "Release ${TAG}: https://github.com/${RELEASE_REPO}/releases/tag/${TAG}"

echo "== commit =="; git -C "${REPO}" show --stat --format='%an <%ae>%n%B' HEAD
echo "== files =="; (cd "${REPO}" && find "${OPDIR}" -type f | sort)

if [ "${DRY_RUN}" = 1 ]; then
  echo "DRY_RUN=1: not pushing; repository left at ${REPO}"; exit 0
fi

# 6. push to the fork and open the PR upstream
git_fork remote add fork "https://github.com/${FORK}.git"
git_fork push fork "${BRANCH}"
gh_community pr create --repo "${UPSTREAM}" --head "${FORK_OWNER}:${BRANCH}" --base main \
  --title "${SUBJECT}" \
  --body "Release ${TAG} of ${OPERATOR}: https://github.com/${RELEASE_REPO}/releases/tag/${TAG}

Adds \`${OPDIR}/${VERSION}/\` (bundle manifests, metadata and tests unchanged from the release's bundle tarball, checksum-verified) and its \`release-config.yaml\` for auto-release into the \`semver.yaml\` template, channel Stable."
