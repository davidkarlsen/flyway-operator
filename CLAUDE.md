# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository. The project facts, commands, architecture and conventions shared by every agent live in `AGENTS.md`, imported here. Directory-specific notes are in the nested `AGENTS.md` files.

@AGENTS.md

## Workflows (skills and agents)

Repo workflows live in `.claude/skills/` and build on the oh-my-claudecode agents; they hold the repo-specific rules, so read the relevant one before acting:

| Skill | Use for |
|---|---|
| `issue-to-pr` | feature/bug from issue to a PR that closes it (design review, worktree, tests incl. kind e2e lock, PR conventions, model selection) |
| `review-loop` | independent critic + code-reviewer rounds until approval, then the merge rules |
| `operator-release` | readiness check, tag-build verification, Helm chart release PR, post-release gate |
| `tooling-upgrade` | one PR per outdated tool (Go, operator-sdk, controller-tools, envtest, kustomize, opm, default Flyway image) |
| `security-sweep` | govulncheck, Dependabot/code-scanning alerts, Scorecard |

Agents in `.claude/agents/`: `operator-developer` (implements issues) and `operator-release-manager` (releases, upgrades, security). Reviews reuse the OMC `critic`, `code-reviewer` and `architect` agents.

## Prerequisite: oh-my-claudecode

The skills assume the oh-my-claudecode (OMC) plugin for the `critic`, `code-reviewer`, `architect`, `executor`, `plan`, `autopilot` and `ralph` agents and skills. Without OMC, use the built-in `general-purpose` agent with the same instructions. OMC's directory-context hook is also what delivers the nested `AGENTS.md` files; without it only this file and the root `AGENTS.md` are read.

## Kind e2e lock

Several agents may share the machine, and the kind e2e suite (`make test-e2e`) builds a fixed image tag and defaults to one cluster and registry. Take the shared lock (`/tmp/flyway-e2e.lock`), use a unique `KIND_CLUSTER` / `E2E_REGISTRY_NAME` / `E2E_REGISTRY_PORT` (never 5001), run it in the background (the suite takes about 25 minutes), and never delete clusters or registries you did not create. The exact lock block is in step 3 of `.claude/skills/issue-to-pr/SKILL.md`.
