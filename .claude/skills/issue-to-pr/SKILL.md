---
name: issue-to-pr
description: Take a flyway-operator change from idea to a reviewed pull request - write or refine the GitHub issue, review the design for features, implement in a worktree, run the full local checks (unit, envtest, kind e2e under the shared lock), and open a PR that closes the issue. Use when asked to implement a feature, fix a bug, or "make an issue and a PR" in this repo.
---

# issue-to-pr

Repo-specific workflow on top of the OMC agents. Don't re-implement what they do; call them.

## Model selection

Pass `model` explicitly when delegating (tier aliases):

| Step | Agent | Model |
|---|---|---|
| Design review of a feature issue | `oh-my-claudecode:architect`, `oh-my-claudecode:critic` | `opus` |
| Planning a vague or large request | `oh-my-claudecode:plan` / `planner` | `opus` |
| Implementing a feature (API, reconcile behaviour, release/CI flow) | `operator-developer` | `opus` |
| Bug fix, chore, dependency or chart fix | `operator-developer` | `sonnet` |
| Pure monitoring (wait for CI, poll a PR) | background shell loop or a monitor agent | `haiku` |

## 1. Issue

- One issue per change, and every PR has one (features, fixes, chart changes, CI and dependency work; only Dependabot PRs and small docs-only fixes are exempt). Always label it with a type and one or more areas: `gh issue create --label enhancement|bug --label area/<x>`. Areas: `area/api` (CRD/types), `area/controller` (reconcile, Job, status), `area/helm`, `area/olm` (bundle/catalog), `area/observability` (metrics, alerts, dashboards), `area/security`, `area/ci` (workflows, release, tooling), `area/docs`. Pick every area the change touches; create a new `area/*` label only when none fits. Before creating one, check `gh issue list` for an existing issue the change already fixes (e.g. a spec issue) and use that instead.
- Body: problem, proposal (API/fields if any), behaviour, acceptance criteria (testable), out of scope. Short.
- **Features** (new API fields, reconcile behaviour, release/CI flow): get the design reviewed *before* coding. Run `oh-my-claudecode:architect` and `oh-my-claudecode:critic` in parallel on the issue, each independently. Fold their findings into the issue body (edit, don't comment). Bring real trade-offs to the user as a decision, with a recommendation.
- Larger or vague requests: start with the `oh-my-claudecode:plan` skill instead.
- Bugs and small chores: skip the design review.

## 2. Implement

Delegate to `operator-developer` (or `oh-my-claudecode:executor`) with `isolation: worktree`, branch off the latest `origin/main`. The brief must include the issue number and these repo rules:

- API changes: kubebuilder markers, then `make manifests generate`. Commit the regenerated `config/crd/bases`; CI fails on drift (the drift check comes from #498).
- CEL rules need Kubernetes 1.25 or later (they're silently ignored before that) and must install on the oldest supported version (`minKubeVersion` in `config/manifests/bases/*.clusterserviceversion.yaml`). New CEL must install on the `minKubeVersion` in `config/manifests/bases/*.clusterserviceversion.yaml` (currently 1.35.0); test the CRD with envtest at that version (`bin/setup-envtest use <version>`). Avoid `lowerAscii()` and other cost-heavy CEL; prefer schema markers (`MaxLength`, `Pattern`) where they suffice.
- Docs live in `docs/`: user-facing behaviour in `docs/usage.md`, install/requirements in `docs/installing.md`, metrics in `docs/metrics.md`. The docs-examples envtest strictly validates every `Migration` in `docs/` and requires `config/samples/` to stay a subset of the `docs/usage.md` example.
- Don't touch `config/helm-chart/` in feature PRs: merging chart changes publishes the chart. Chart changes go in the post-release chart PR (see the `operator-release` skill).
- Don't hand-regenerate `bundle/manifests` in feature PRs; CI regenerates the bundle on main pushes and tags. Tooling bumps may update `bundle/` labels and scorecard image tags.
- Operator annotations/labels use the `Prefix` constant.

### Large features: autopilot or ralph

For a large, well-specified feature (reviewed issue, clear acceptance criteria), implementation may be handed to `oh-my-claudecode:autopilot` (or `oh-my-claudecode:ralph` when it should loop until verification passes) instead of a single `operator-developer` run. Its own critic and validation passes then run on top of this workflow. Brief it with these constraints, which override its defaults:

- Scope is the issue's acceptance criteria. Nothing else.
- Work in a worktree on a branch off `origin/main`; follow the rules in step 2 and the e2e lock in step 3.
- Stop at an open PR with `Closes #N`. Never merge, enable auto-merge, push tags, or change repository settings.
- Hand the PR to the `review-loop` skill. The independent critic and code-reviewer rounds still apply, regardless of autopilot's internal reviews.
- Ask the user, rather than deciding, when a finding changes the API shape, defaults or release order.

Don't use autopilot for chores, dependency bumps or docs; a single agent is cheaper.

## 3. Verify locally

```sh
make test                                 # manifests, generate, fmt, vet, unit + envtest
golangci-lint run --build-tags e2e ./...
```

Kind e2e when the change affects the Job, reconcile behaviour or anything a real cluster would show. **Several agents may share the machine**: the suite builds a fixed image tag and defaults to one cluster/registry, so take the lock, use unique names, and run the block **in the background** (the suite can take ~25 minutes, longer than a foreground shell call; a killed shell skips the `EXIT` trap):

```sh
L=/tmp/flyway-e2e.lock
until mkdir "$L" 2>/dev/null; do
  # break a stale lock whose owner process is gone
  p=$(cat "$L/pid" 2>/dev/null)
  if [ -n "$p" ] && ! kill -0 "$p" 2>/dev/null && [ "$(cat "$L/pid" 2>/dev/null)" = "$p" ] && mv "$L" "$L.stale.$$" 2>/dev/null; then rm -rf "$L.stale.$$"; continue; fi
  sleep 15
done
echo $$ > "$L/pid"; trap 'rm -rf "$L"' EXIT
T=<topic>; PORT=$((5100 + $(printf '%s' "$T" | cksum | cut -d' ' -f1) % 900))   # unique per topic, never 5001
make test-e2e KIND_CLUSTER=e2e-$T E2E_REGISTRY_NAME=e2e-$T-registry E2E_REGISTRY_PORT=$PORT
make cleanup-test-e2e KIND_CLUSTER=e2e-$T E2E_REGISTRY_NAME=e2e-$T-registry
```

Never delete clusters or registries you didn't create. If a run crashed, clean up your own `e2e-<topic>` cluster and registry before retrying. Prove a new test bites: break the code it guards, see it fail, restore.

## 4. Pull request

- Rebase on `origin/main` right before pushing.
- `gh pr create` with `Closes #<issue>` in the body for **every** issue the PR resolves (one line each), so the merge closes them. Then verify GitHub registered the link: `gh pr view <n> --json closingIssuesReferences -q '[.closingIssuesReferences[].number]'` must list each issue; if not, fix the body.
- Stacked PRs (built on another open PR) each close their own issue.
- Body describes the change **as it now stands**: what changed and what it does, what the reviewer must check, risks that survive the merge. No preamble, no investigation history, no rejected options.
- Commit messages follow the same rule. Sign off (`git commit -s`).
- Open as **draft** if it must wait (e.g. for a release); say why in the body.

Then hand off to the `review-loop` skill. Don't merge from here.
