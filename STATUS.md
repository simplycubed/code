# Status

SimplyCubed Code is at the very beginning. This file is the honest running
account of what exists, what is deliberately deferred, and what a maintainer
needs to do next. It is written to be read first.

## What this branch adds (`build/foundation`)

An overnight foundation pass, built engine-first so the whole loop is testable
with no model calls. The scope line, on advice: **code the engine-independent
layers; write the engine-dependent ones as docs**, because the parts that depend
on a real model (prompt wording, budget numbers, stall thresholds, packaging) are
exactly what the blocked Azure replay spike exists to validate. Coding them now
would bake in guesses.

See `loop/goal.json` for the machine-readable goal, invariants, and milestones.

## What is coded now (M1 through M6, all green)

- **Gate and CI** (`Makefile`, `.github/workflows/check.yml`).
- **`internal/domain`** — core types with no model or GitHub dependency.
- **`internal/engine`** — the `Runner` interface, plus a deterministic fake that
  drives every test.
- **`internal/gate`** — runs a repo gate command; captures exit code, an output
  tail, and a normalized signature for stall detection.
- **`internal/config`** — loads `.github/simplycubed.yml` and refuses a config
  with no `gate:`.
- **`internal/state`** — the `sc:` label lifecycle with mutual-exclusion
  transitions.
- **`internal/loop`** — the engine: goal to act to grade to repeat, opening a PR
  on success and escalating (Blocked, no PR) on a stall. Includes the honesty
  test.
- **`internal/forge`** — the GitHub side as an interface (no merge method) with a
  recording fake.
- **`internal/ledger`** — append-only JSONL run events, wired into the loop.

The whole loop, including every failure path, runs with no model and no network.

## What remains, and why it is not coded here

Everything left is either blocked on you (the Azure spikes S1 through S4) or
deliberately deferred to `docs/decisions` because it depends on how a real model
behaves, which the S3 replay measures: the engine adapter, role prompts, the
verdict schema, budget and stall numbers, and the GitHub Action packaging. This
is the code-vs-docs line from the review, held on purpose.

## Gate

`make check` is the gate: `gofmt`, `go vet`, `go build`, `go test`. It runs in
CI via `.github/workflows/check.yml`. Every commit on this branch is meant to be
green, so the branch can be reviewed commit by commit.

## Deliberately NOT done (maintainer decisions)

- **`make check` is not yet a required status check on `main`.** Left for you.
  Making it required with an empty bypass list while you are the only account can
  deadlock your own PRs (a PR author cannot approve their own PR). Resolve when a
  bot identity or second reviewer exists, or add yourself as a bypass actor and
  stop calling it no-bypass.
- **No repository settings were changed overnight** (no ruleset, approval, or
  branch-protection edits). Those are the one class of change a sleeping user
  cannot easily undo.
- **CI actions are pinned by tag, not commit SHA.** Pin by SHA before this goes
  past scaffolding.

## Blocked on you

The Phase 0 validation spikes are blocked on Azure access and were not faked:

- **S1 (Codex + Azure auth):** `codex` CLI is installed (0.146.0). The Azure
  connection needs three non-secret names from you (resource name, gpt-5.4
  deployment name, gpt-5.4-mini deployment name) and the `AZURE_OPENAI_API_KEY`
  set in a scratch env file. An isolated `CODEX_HOME` config template is staged
  in the session scratch dir so your existing `~/.codex` is untouched.
- **S3 (replay against a real private repo):** the high-signal test of whether
  GPT-5.4 can clear a real gate, by reverting known-good commits and checking
  whether the loop reproduces them. Blocked by S1.

Nothing in the codebase depends on these; they validate assumptions the ADRs
record rather than the code.

## Layout

```
cmd/simplycubed/      CLI entry (version; local run/debug later)
internal/buildinfo/   build-time version
internal/domain/      core types (M2)
internal/engine/      Runner interface + fake engine (M2)
internal/gate/        runs a repo gate command, captures result (M3)
internal/config/      .github/simplycubed.yml; refuses a missing gate (M3)
internal/state/       sc: label lifecycle (M3)
internal/loop/        goal -> act -> grade -> repeat; PR only on success (M4)
docs/spec/            architecture
docs/decisions/       ADRs, including the engine-dependent design (M5)
loop/goal.json        the goal contract for this build
```
