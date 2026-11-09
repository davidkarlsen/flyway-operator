#!/usr/bin/env bash
# Submit a flyway-operator release to the OpenShift Community Operators catalog
# (redhat-openshift-ecosystem/community-operators-prod): add operators/flyway-operator/<version>/ from the
# release's bundle tarball plus a release-config.yaml, and open a PR from the fork. The upstream pipeline then
# builds the bundle and, after merge, updates the semver template and the catalogs itself.
#
# Usage: hack/community-release.sh vX.Y.Z
# Environment:
#   DRY_RUN=1            work from upstream main and stop before the fork is synced or anything is pushed; needs no community token
#   GH_TOKEN             read access to the flyway-operator release (gh release download)
#   COMMUNITY_TOKEN      token with push rights on the fork and PR rights upstream; required unless DRY_RUN=1
#   WORKDIR              scratch directory, must not exist or be empty (default: a new mktemp dir)
#   RELEASE_REPO / COMMUNITY_UPSTREAM / COMMUNITY_FORK   repositories (defaults below)
#   UPSTREAM_URL / FORK_URL   clone URLs (default https://github.com/<repo>.git; for tests)
#
# Re-running is safe: it stops when upstream has the version or a PR exists for the branch, and it opens the
# missing PR when the branch was pushed but the PR step failed.
set -euo pipefail

TAG="${1:?usage: $0 vX.Y.Z}"
[[ "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "not a release tag (pre-releases are not submitted): ${TAG}" >&2; exit 1; }
VERSION="${TAG#v}"

RELEASE_REPO="${RELEASE_REPO:-davidkarlsen/flyway-operator}"
UPSTREAM="${COMMUNITY_UPSTREAM:-redhat-openshift-ecosystem/community-operators-prod}"
FORK="${COMMUNITY_FORK:-davidkarlsen/community-operators-prod}"
UPSTREAM_URL="${UPSTREAM_URL:-https://github.com/${UPSTREAM}.git}"
FORK_URL="${FORK_URL:-https://github.com/${FORK}.git}"
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
    GIT_CONFIG_VALUE_0="AUTHORIZATION: basic ${auth}" git "$@"
}
gh_community() { GH_TOKEN="${COMMUNITY_TOKEN}" gh "$@"; }
sha256_check() { if command -v sha256sum >/dev/null; then sha256sum -c -; else shasum -a 256 -c -; fi; }

# 1. bundle tarball: checksums.txt is signed by this repo's release workflow for this tag, the tarball matches it
tarball="flyway-operator-bundle-${TAG}.tar.gz"
gh release download "${TAG}" -R "${RELEASE_REPO}" -D "${DL}" -p "${tarball}" -p checksums.txt -p checksums.txt.sigstore.json
identity="^https://github\\.com/${RELEASE_REPO//./\\.}/\\.github/workflows/build\\.yaml@refs/tags/${TAG//./\\.}\$"
cosign verify-blob "${DL}/checksums.txt" --bundle "${DL}/checksums.txt.sigstore.json" \
  --certificate-identity-regexp "${identity}" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
(cd "${DL}" && grep -F " ${tarball}" checksums.txt | sha256_check)
tar -xzf "${DL}/${tarball}" -C "${BUNDLE}" bundle/manifests bundle/metadata bundle/tests
for d in manifests metadata tests; do [ -d "${BUNDLE}/bundle/${d}" ] || { echo "bundle/${d} missing from ${tarball}" >&2; exit 1; }; done

# 2. upstream state: the first submission must be merged, and the version must not be there yet
upstream_has() { gh api -X GET "repos/${UPSTREAM}/contents/$1" -f ref=main --silent 2>/dev/null; }
if ! upstream_has "${OPDIR}/ci.yaml"; then
  if [ "${DRY_RUN}" = 1 ]; then
    echo "warning: ${UPSTREAM} has no ${OPDIR}/ci.yaml (first submission not merged); a real run would stop here" >&2
  else
    echo "error: ${UPSTREAM} has no ${OPDIR}/ci.yaml: the first submission is not merged yet; submit ${TAG} by hand or re-run after it merges" >&2
    exit 1
  fi
fi
if upstream_has "${OPDIR}/${VERSION}"; then
  echo "skip: ${OPDIR}/${VERSION} already exists upstream"; exit 0
fi

# 3. working clone: the fork after syncing its main (so the base commit exists on the push target); upstream when dry
if [ "${DRY_RUN}" = 1 ]; then
  git clone -q --depth 1 --filter=blob:none --sparse --branch main "${UPSTREAM_URL}" "${REPO}"
else
  gh_community repo sync "${FORK}" --branch main
  git_fork clone -q --depth 1 --filter=blob:none --sparse --branch main "${FORK_URL}" "${REPO}"
fi
git -C "${REPO}" sparse-checkout set "${OPDIR}"

# Read-only lookups (also in a dry run, with the default gh login and an unauthenticated ls-remote):
# a PR from this fork for the branch, in any state so a merged one also counts, and the branch on the fork.
# The head filter takes the branch name only, hence the owner filter.
lookup() { if [ "${DRY_RUN}" = 1 ]; then gh "$@"; else gh_community "$@"; fi; }
prs="$(lookup pr list --repo "${UPSTREAM}" --head "${BRANCH}" --state all --json url,headRepositoryOwner \
  --jq ".[]|select(.headRepositoryOwner.login==\"${FORK_OWNER}\")|.url")"
# only exit status 2 of ls-remote means "no such branch"; anything else is an error
rc=0
if [ "${DRY_RUN}" = 1 ]; then git ls-remote --exit-code --heads "${FORK_URL}" "${BRANCH}" >/dev/null || rc=$?
else git_fork -C "${REPO}" ls-remote --exit-code --heads origin "${BRANCH}" >/dev/null || rc=$?; fi
case "${rc}" in 0) branch_on_fork=1 ;; 2) branch_on_fork=0 ;; *) echo "error: git ls-remote failed (${rc})" >&2; exit 1 ;; esac

pr_body="Release ${TAG} of ${OPERATOR}: https://github.com/${RELEASE_REPO}/releases/tag/${TAG}

Adds \`${OPDIR}/${VERSION}/\` (bundle manifests, metadata and tests unchanged from the release's bundle tarball, checksum-verified) and its \`release-config.yaml\` for auto-release into the \`semver.yaml\` template, channel Stable."
create_pr() {
  gh_community pr create --repo "${UPSTREAM}" --head "${FORK_OWNER}:${BRANCH}" --base main --title "${SUBJECT}" --body "${pr_body}"
}

if [ -n "${prs}" ]; then echo "skip: PR already exists: ${prs}"; exit 0; fi
if [ "${branch_on_fork}" = 1 ]; then
  # an earlier run pushed the branch but did not open the PR
  if [ "${DRY_RUN}" = 1 ]; then
    echo "DRY_RUN=1: branch ${BRANCH} exists on ${FORK} without a PR; a real run would open it"; exit 0
  fi
  git_fork -C "${REPO}" fetch -q --depth 1 origin "${BRANCH}"
  git -C "${REPO}" cat-file -e "FETCH_HEAD:${OPDIR}/${VERSION}/release-config.yaml" \
    || { echo "error: branch ${BRANCH} on ${FORK} lacks ${OPDIR}/${VERSION}; delete it and re-run" >&2; exit 1; }
  echo "branch ${BRANCH} exists on ${FORK} without a PR: opening it"
  create_pr
  exit 0
fi

# 4. branch, version directory (manifests, metadata, tests unchanged) and release-config.yaml
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

# 6. push to the fork, then open the PR upstream
git_fork -C "${REPO}" push origin "${BRANCH}"
create_pr
