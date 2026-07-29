# 0001. Engine-independent core

Status: Accepted, implemented.

## Context

The system calls a model to write code. Models are expensive to call, non-
deterministic, and swappable. If the loop, the gate, the config, and the state
machine all depended directly on a live model, none of them could be tested
without spend, and the vendor could not be changed without rewriting the core.

## Decision

The model sits behind a single interface, `engine.Runner`, with one method: run a
role's turn in a working tree and report what it did. The loop depends only on
this interface. A deterministic fake in `internal/engine/fake` drives every test,
performing no network calls.

Everything except the concrete engine adapters is written to compile and pass
tests with the fake: domain types, the gate runner, config parsing, the label
state machine, and the loop itself.

## Consequences

- The whole control flow, including its failure paths, is testable at zero cost.
  The honesty test (a never-fixing engine must end Blocked and open no PR) runs
  in milliseconds.
- Swapping Codex for Claude Code, or for anything else, is one implementation of
  one interface plus a config string.
- The parts that genuinely depend on model behavior (prompt text, verdict shape,
  budget numbers) are held out of the core and recorded as pending decisions
  rather than coded against a guess.
