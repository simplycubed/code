# Architecture

SimplyCubed Code turns a GitHub issue into a reviewed pull request, running
inside the adopter's own GitHub Actions. This document describes the shape of the
system. It is a design under construction, not a description of finished code.
Where the code exists, it matches this; where it does not yet, this is the
target.

## One sentence

A gate runner that happens to call a model: the loop never defines "done" (the
repo's own gate does), and there is no code path that merges.

## Principles

- **The agent proposes; a human disposes.** The agent's strongest action is
  opening a pull request. It never merges, never pushes to a protected branch,
  and holds no deploy or production credential. Merge is always a human decision.
  This holds whatever model backs the engine.
- **Gate-first.** A loop is `goal -> act -> grade -> repeat`, where "grade" is the
  repo's own gate command (typecheck, tests, build). The engineering is the gate,
  not the loop. A repo with no gate is refused.
- **Engine-independent core.** The model sits behind a `Runner` interface. The
  loop, gate, config, state machine, and domain types compile and test against a
  deterministic fake, with no network and no spend. This is what makes the vendor
  pluggable and the system testable.
- **GitHub is the whole surface.** State lives in labels, PRs, and comments. There
  is no server and no database. A human drives the agent by applying one label and
  reviewing a pull request.

## Components

- **`domain`** — the core types: an issue to work, a role, a run request and
  result, a verdict, a gate result.
- **`engine` (`Runner`)** — the seam to the model. One method: run a role's turn
  in a working tree and return what it did. Implementations shell out to a coding
  agent CLI. The first real adapter targets the Codex CLI against Azure OpenAI; a
  Claude Code adapter is planned. A `fake` implementation drives all tests.
- **`gate`** — runs the repo's configured gate command, captures the exit code, a
  bounded tail of output, and a normalized signature (used to detect a loop that
  is failing the same way every iteration rather than making progress).
- **`config`** — loads `.github/simplycubed.yml`. The `gate:` command is required;
  a config without it is an error, not a default to paper over.
- **`state`** — the label lifecycle. `sc:go` is the only human-applied label; the
  bot drives `sc:queued -> sc:working -> sc:review -> sc:blocked | sc:done`. States
  are mutually exclusive; the bot removes the prior label. The prefix is
  configurable (`labelPrefix`, default `sc`).
- **`loop`** — the engine: run a role, grade against the gate, feed failure back,
  repeat until the gate passes or the loop stalls. It opens a pull request only on
  success. On a stall it escalates (labels the issue for a human) and opens no PR.
- **`forge`** — the GitHub side (issues, labels, PRs, comments). An interface, so
  the loop is testable without touching GitHub.

## Roles

Two to start: an **implementer** that edits and runs the gate, and a read-only
**reviewer** that judges the diff and emits a structured verdict. A fixer is the
implementer resumed against the reviewer's findings. The reviewer posts a comment
review only; it never approves or requests-changes as a GitHub review, because the
merge gate is CI plus a human, never a bot's own approval.

## Runtime

The product ships as a published GitHub Action plus reusable workflows. One role
turn is one Actions job, triggered by an issue label or a review event, chained
through the events each turn emits. State is GitHub; the ledger is a file on an
orphan branch. Nothing is hosted by SimplyCubed.

## What is intentionally deferred

The role prompt text, the structured verdict schema, the per-role budget and
stall thresholds, and the Action packaging are documented as decisions (see
`docs/decisions/`) but not yet coded, because the Azure replay spike is what will
tell us whether the assumptions behind them hold. They are cheap to revise as
prose and expensive to revise as code written against a guess.
