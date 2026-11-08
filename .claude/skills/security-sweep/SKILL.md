---
name: security-sweep
description: Security review of flyway-operator's dependencies and repo setup - reachable Go vulnerabilities (govulncheck), Dependabot and code-scanning alerts, OpenSSF Scorecard, and fixes as PRs. Use when asked about vulnerabilities, Dependabot/malware settings, Scorecard or "is it secure".
---

# security-sweep

## Checks

```sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...                 # reachable vulnerabilities (what matters)
go run golang.org/x/vuln/cmd/govulncheck@latest -show verbose ./...   # incl. unreachable
gh api "repos/davidkarlsen/flyway-operator/dependabot/alerts?state=open" --jq length
gh api "repos/davidkarlsen/flyway-operator/code-scanning/alerts?state=open" --jq length
gh api repos/davidkarlsen/flyway-operator --jq .security_and_analysis
GITHUB_AUTH_TOKEN="$(gh auth token -h github.com)" scorecard --repo=github.com/davidkarlsen/flyway-operator --show-details
```

Install the `scorecard` CLI (e.g. from its GitHub releases or a package manager). Never print tokens; pipe them as above.

govulncheck finds reachable vulnerabilities that Dependabot may not alert on (Dependabot has no reachability analysis and lags the Go vuln DB). CI also runs govulncheck as its own job, gating PRs but not releases.

## Fixing

- Reachable vulnerabilities: a PR bumping the module to the fixed version (`go get <mod>@<fixed> && go mod tidy`), proving `govulncheck` is clean after. Prefer the minimal bump; list collateral module changes in the PR body.
- Unreachable ones: batch into a periodic bump PR; don't rush.
- Scorecard: `Code-Review` stays low for a solo maintainer (self-merged PRs). Don't game it. Keep `Pinned-Dependencies` (SHA pins), `Token-Permissions` (top-level `contents: read`), `Security-Policy` (`SECURITY.md`) and `Signed-Releases` (signed tag releases and chart releases) intact when editing workflows.
- Dependabot malware alerts only cover npm today; not relevant for this repo's Go/Docker/Actions ecosystems.

Open fixes via `issue-to-pr` (routine bumps are exempt from the issue step, see `issue-to-pr` §1; vulnerability fixes beyond a plain bump get an issue labelled `bug` and `area/security`) and review with `review-loop`.
