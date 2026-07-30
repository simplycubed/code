# Status

The honest running account of what exists, what is deferred, and what a
maintainer does next. Read this first. For the product overview see the README.

## What works today

Two loops run end to end via the CLI, on the Codex-on-Azure engine (GPT-5.4),
driven entirely through GitHub:

- **Issue to pull request** (`simplycubed run <owner/repo#N>`). The implementer
  works in an isolated worktree, retries against the repo's own gate until it
  passes, commits and pushes, and opens a pull request. On a stall it escalates:
  it labels the issue for a human and opens no pull request. It never merges.
- **Fix on request** (`simplycubed address <owner/repo#PR>`). It reads the human
  review feedback on an open pull request; a fixer role addresses it against the
  gate and pushes back to the same branch for another look. Feedback is filtered
  to the current head commit, so an already-addressed comment is never
  re-litigated. It opens no new pull request and never merges.

Both were dogfooded: the issue-to-PR loop produced the merged dependency-upgrade
PR on `charlesgreen/gsm`.

## Layers (all green under `make check`)

```
cmd/simplycubed/      CLI: version, run, address
internal/domain/      core types (Role, Issue, RunRequest, ReviewFeedback, Verdict)
internal/engine/      Runner interface + fake; codex/ adapter (Azure OpenAI)
internal/gate/        runs the repo gate command; exit code, output tail, signature
internal/config/      .github/simplycubed.yml; refuses a missing gate; attribution flag
internal/state/       sc: label lifecycle with mutual exclusion
internal/roles/       implementer, reviewer, fixer as data; bounds; untrusted-input delimiting
internal/loop/        goal -> act -> grade -> repeat; Run (issue->PR) and Fix (fix-on-request)
internal/forge/       GitHub side as an interface (no merge method); gh/ adapter + recording fake
internal/vcs/git/     commit, push, and sync-to-PR-head
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
via `.github/workflows/check.yml` and is the required check on `main`.

## What is next

- **Wire the read-only reviewer role into the loop.** The `Reviewer` role and the
  `Verdict` type exist, but no loop calls the reviewer yet, so today the review is
  the human's. Wiring it (reviewer emits a structured verdict, findings feed the
  fixer, comment-only, never a bot approval) is the next core-loop step. The
  README says so plainly rather than implying it is already done.
- **Self-onboarding via a bootstrap issue and setup pull requests** (the
  `init`/onboard capability): the product proposes its own config, labels, and
  workflow files as pull requests a human merges.
- **Engine roadmap:** Codex on Azure (now) -> a Claude Code adapter -> Hugging
  Face self-hosted models. Each is another `Runner` implementation; no core
  rework.

## Deliberately not done (maintainer decisions)

- **CI actions are pinned by tag, not commit SHA.** Pin by SHA before this goes
  past scaffolding.
- **Per-role bot identities do not exist yet.** Runs use the operator's own gh
  auth. The design carries the per-role identity as configuration so adding it is
  a config change, not a refactor.
