---
name: review-loop
description: Review a flyway-operator pull request with independent reviewers, send findings back to the author, repeat until approved, then merge by the repo's rules. Use whenever a PR is opened or updated in this repo, or when asked to "review", "get it approved" or "merge when green".
---

# review-loop

## Reviewers

Run in parallel, as separate background agents, each **independently** (never pass one reviewer's output to the other, never pass the author's reasoning):

| PR type | Reviewers |
|---|---|
| Feature or behaviour change | `oh-my-claudecode:critic` + `oh-my-claudecode:code-reviewer` (+ `oh-my-claudecode:architect` for design-heavy changes) |
| Bug fix, CI/workflow, dependency, chart | `oh-my-claudecode:critic` + `oh-my-claudecode:code-reviewer` |
| Docs-only (`*.md`, `docs/`) | none required |

Give each reviewer: the PR number, the issue/plan it implements, read-only rules (no edits, commits or GitHub comments; temp worktree from `pull/<n>/head`, removed afterwards) and the repo facts that matter (from `issue-to-pr`). Ask for `APPROVE`/`REVISE` with verified findings: severity, file:line, problem, fix. Unverified claims must be labelled as such.

Point reviewers at the risks CI can't see: tag/release paths (only run on a `v*` tag), chart changes (published on merge), CRD changes on the oldest supported Kubernetes, behaviour on upgrade of existing `Migration`s and Jobs.

## Model selection

| PR | Critic | Code reviewer | Architect |
|---|---|---|---|
| Feature, API/CRD, reconcile, release path | `opus` | `opus` | `opus` (design-heavy only) |
| CI/workflow, chart, dependency, build tooling | `sonnet` | `sonnet` | - |
| One-line/config changes (README badges, dependabot.yml) | `sonnet` | `haiku` | - |

Re-review rounds reuse the same reviewer agents (SendMessage) so they keep their context.

## Loop

1. Collect both verdicts, then send the combined findings to the PR's author agent (or a new `operator-developer` with the PR branch). Critical/major/medium: fix. Minor/low: fix if cheap, otherwise say why not.
2. Findings that change scope or a user decision (API shape, defaults, release order): ask the user, with a recommendation.
3. After the author pushes, send each reviewer a round-N message listing what changed; re-review the current head.
4. Repeat until both approve. A rewrite (rebase into a new commit, conflict resolution) after approval gets a short final delta review.

## Merge

- Both approved and CI green: `gh pr merge <n> --auto --squash`. If GitHub says the PR is already clean, `gh pr merge <n> --squash`.
- Docs-only: may be merged directly (`--admin`) without waiting for CI.
- Drafts that wait for a release stay drafts until the user confirms the gate.
- Before merging, check the PR body still matches the final diff; fix the body, not with a comment.
- PRs that edit the same files: merge one at a time; rebase the next onto `main` and let CI and the reviewers re-check it.

After merge: confirm the linked issue closed. Close superseded PRs/issues with a comment linking the replacement.
