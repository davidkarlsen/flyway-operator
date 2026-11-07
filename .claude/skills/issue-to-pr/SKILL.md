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
| Implementing a feature (API, reconcile behaviour, release/CI flow) | `flyway-developer` | `opus` |
| Bug fix, chore, dependency or chart fix | `flyway-developer` | `sonnet` |
| Pure monitoring (wait for CI, poll a PR) | background shell loop or a monitor agent | `haiku` |

## 1. Issue

- One issue per change: `gh issue create --label enhancement|bug`.
- Body: problem, proposal (API/fields if any), behaviour, acceptance criteria (testable), out of scope. Short.
- **Features** (new API fields, reconcile behaviour, release/CI flow): get the design reviewed *before* coding. Run `oh-my-claudecode:architect` and `oh-my-claudecode:critic` in parallel on the issue, each independently. Fold their findings into the issue body (edit, don't comment). Bring real trade-offs to the user as a decision, with a recommendation.
- Larger or vague requests: start with the `oh-my-claudecode:plan` skill instead.
- Bugs and small chores: skip the design review.

## 2. Implement

Delegate to `flyway-developer` (or `oh-my-claudecode:executor`) with `isolation: worktree`, branch off the latest `origin/main`. The brief must include the issue number and these repo rules:

- API changes: kubebuilder markers, then `make manifests generate`. `config/crd/bases` must be committed regenerated; CI fails on drift.
- CEL rules must install on the oldest supported Kubernetes (`minKubeVersion` in `config/manifests/bases/*.clusterserviceversion.yaml`). Check with envtest at that version. Avoid `lowerAscii()` and other cost-heavy CEL; prefer schema markers (`MaxLength`, `Pattern`) where they suffice.
- Docs live in `docs/`: user-facing behaviour in `docs/usage.md`, install/requirements in `docs/installing.md`, metrics in `docs/metrics.md`. The docs-examples envtest strictly validates every `Migration` in `docs/` and requires `config/samples/` to stay a subset of the `docs/usage.md` example.
- Don't touch `config/helm-chart/` in feature PRs: merging chart changes publishes the chart. Chart changes go in the post-release chart PR (see the `release` skill).
- Don't commit `bundle/` (CI regenerates it on tags).
- Operator annotations/labels use the `Prefix` constant.

## 3. Verify locally

```sh
make test                                 # manifests, generate, fmt, vet, unit + envtest
golangci-lint run --build-tags e2e ./...
```

Kind e2e when the change affects the Job, reconcile behaviour or anything a real cluster would show. **Several agents may share the machine**: the suite builds a fixed image tag and defaults to one cluster/registry, so always take the lock and use unique names, in one shell:

```sh
until mkdir /tmp/flyway-e2e.lock 2>/dev/null; do sleep 15; done; trap 'rmdir /tmp/flyway-e2e.lock' EXIT
make test-e2e KIND_CLUSTER=e2e-<topic> E2E_REGISTRY_NAME=e2e-<topic>-registry E2E_REGISTRY_PORT=<unique port>
make cleanup-test-e2e KIND_CLUSTER=e2e-<topic> E2E_REGISTRY_NAME=e2e-<topic>-registry
```

Never delete clusters or registries you didn't create. Prove a new test bites: break the code it guards, see it fail, restore.

## 4. Pull request

- Rebase on `origin/main` right before pushing.
- `gh pr create` with `Closes #<issue>` in the body so the merge closes the issue.
- Body describes the change **as it now stands**: what changed and what it does, what the reviewer must check, risks that survive the merge. No preamble, no investigation history, no rejected options.
- Commit messages follow the same rule. Sign off (`git commit -s`).
- Open as **draft** if it must wait (e.g. for a release); say why in the body.

Then hand off to the `review-loop` skill. Don't merge from here.
