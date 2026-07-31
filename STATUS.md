# Status

The honest running account of what exists, what is deferred, and what a
maintainer does next. Read this first. For the product overview see the README.

## What works today

Two loops run end to end on the Codex-on-Azure engine (GPT-5.4) from the CLI.
The GitHub Actions runtime runs them too, as of #99: before that fix the engine's
sandbox could not start on an Ubuntu 24.04 runner, so every Actions run escalated
without proposing anything, and no pull request had ever been opened from
Actions.

- **Issue to pull request** (`simplycubed run <owner/repo#N>`). The implementer
  works in an isolated worktree, retries against the repo's own gate until it
  passes, commits and pushes, and opens a pull request. On a stall it escalates:
  it labels the issue for a human and opens no pull request. It never merges.
- **Fix on request** (`simplycubed address <owner/repo#PR>`). It reads the human
  review feedback on an open pull request; a fixer role addresses it against the
  gate and pushes back to the same branch for another look. Feedback is filtered
  to the current head commit, so an already-addressed comment is never
  re-litigated. It opens no new pull request and never merges.

The CLI path is still the easiest way to prove a repository config locally. The
Actions path is event-driven: the adopter calls the reusable workflow, `sc:go`
starts issue-to-PR work, and submitted pull-request reviews trigger the
fix-on-request loop inside that repository's own Actions.

The Actions runtime authenticates as the `simplycubed-code[bot]` GitHub App.
Each job mints its own installation token scoped to one repository with
`contents`, `issues`, and `pull-requests` permissions only.

`v0.1.9` is the current release. `go install
github.com/simplycubed/code/cmd/simplycubed@v0.1.9` works today. `v0.1.2` and `v0.1.4` are retracted in `go.mod` because those tags pointed at the wrong
commits.

Upgrade from anything earlier. `v0.1.7` has a duplicate `env:` key in its copy
of the reusable workflow, which GitHub refuses to load, so every run under it
fails in about a second without starting a job. `v0.1.8` loads and runs, but its
engine sandbox cannot start on an Ubuntu 24.04 runner, so every Actions run
under it escalates without proposing anything. `v0.1.9` is the first release
whose Actions runtime can do work.

Both loops are dogfooded here. The issue-to-PR loop, running in this
repository's own Actions, produced PR #102 against issue #95: the fix, the
tests, and the documentation for it, reviewed and merged by a human. That is the
first pull request the agent has opened from Actions.

## Layers (all green under `make check`)

```
cmd/simplycubed/      CLI: version, init, preflight, run, address
internal/domain/      core types (Role, Issue, RunRequest, ReviewFeedback, Verdict)
internal/engine/      Runner interface + fake; codex/ (Azure OpenAI) and claude/ adapters
internal/gate/        runs the repo gate command; exit code, output tail, signature
internal/config/      .github/simplycubed.yml; refuses a missing gate; engine, review,
                      prDescription, attribution
internal/state/       sc: label lifecycle with mutual exclusion
internal/roles/       implementer, reviewer, fixer as data; bounds; untrusted-input delimiting
internal/loop/        goal -> act -> grade -> repeat; Run (issue->PR) and Fix (fix-on-request)
internal/forge/       GitHub side as an interface (no merge method); gh/ adapter + recording fake
internal/vcs/git/     commit, push, and sync-to-PR-head
internal/describe/    generated PR walkthrough, changes table, and sequence diagram
internal/verdict/     the reviewer verdict: schema, validation, findings for the fixer
internal/command/     comment commands (@simplycubed-code go | address | help)
internal/forge/dryrun/ records the GitHub writes a dry run skips
internal/attribution/ the SimplyCubed Code marker on generated commits and PRs
internal/ledger/      append-only JSONL run events
internal/worktree/    an isolated git worktree per issue
docs/spec/            architecture
docs/decisions/       ADRs
loop/goal.json        the machine-readable goal contract
```

The whole loop, including every failure path, is tested with no model and no
network, behind the `Runner` interface with a deterministic fake. The gh adapter
and the codex adapter have their own tests (a gh stub, a fake codex).

## Gate

`make check` is the gate: `gofmt`, `go vet`, `go build`, `go test`. It runs in CI
via `.github/workflows/check.yml`; the required status check on `main` is the
`checks-pass` aggregate from that workflow.

## What is next

- **Engine roadmap:** Codex on Azure is the engine the reusable GitHub Actions
  workflow can actually run. The Claude Code adapter is written and
  `engine: claude` now works from a local CLI run, but the reusable workflow
  still installs only the Codex CLI and still requires Azure inputs and
  secrets, so the Actions path cannot run Claude yet. See #95. Self-hosted
  models on Hugging Face are next. Each is another `Runner` implementation; no
  core rework.

## Deliberately not done (maintainer decisions)

Nothing outstanding. The two items that lived here, SHA-pinned actions and a
per-role bot identity, both shipped in v0.1.3 and v0.1.6.
