---
name: tooling-upgrade
description: Bring flyway-operator's toolchain up to date - Go, operator-sdk, controller-tools (controller-gen), envtest Kubernetes, kustomize, opm, golangci-lint, the default Flyway image - with one PR per tool. Use when asked "are we on the latest Go/tooling?", "upgrade the tooling" or on a periodic check.
---

# tooling-upgrade

## Where versions live

| Tool | Location |
|---|---|
| Go | `go.mod` `go` directive (CI reads it via `go-version-file`), `Dockerfile` builder image |
| operator-sdk | `Makefile` `OPERATOR_SDK_VERSION`; `bundle.Dockerfile` + bundle metadata builder labels; `scorecard-test` image tags in `config/scorecard/` and `bundle/tests/scorecard/` |
| opm | `Makefile` opm download URL |
| controller-tools | `Makefile` `CONTROLLER_TOOLS_VERSION` (regenerate: `make manifests generate`) |
| envtest | `Makefile` `ENVTEST_K8S_VERSION` (match the `k8s.io/*` minor in `go.mod`) |
| kustomize | `Makefile` `KUSTOMIZE_VERSION` |
| golangci-lint | CI action installs latest; nothing to pin |
| GitHub Actions | SHA-pinned with version comments; Dependabot (grouped) keeps them current |
| Default Flyway image | `defaultFlywayImage` in `internal/controller/jobutil.go` (floating major tag) + `docs/usage.md` |

## Steps

1. Check latest versions: `curl -s 'https://go.dev/dl/?mode=json' | jq -r '.[0].version'`; `gh api repos/<owner>/<repo>/releases/latest --jq .tag_name` for the others; `setup-envtest list` / the controller-tools envtest releases file for envtest.
2. Report what's behind before changing anything.
3. One branch and PR per tool (via `issue-to-pr` without the issue step for routine bumps), each verified with `make test`; tools that regenerate output (controller-gen, operator-sdk) must leave only the version annotation as a diff, or explain the rest.
4. Major version jumps (e.g. Flyway 10→13, a new Go minor with language changes): read the release notes for anything the operator relies on (flags, env vars, defaults) and run a real migration (kind e2e under the shared lock, or `docker run` against a throwaway Postgres) before opening the PR. Create an issue for these.
5. Review with `review-loop` (critic + code-reviewer, light models for pure version bumps); merge once both approve and CI is green.
