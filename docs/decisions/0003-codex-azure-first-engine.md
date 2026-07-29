# 0003. Codex on Azure as the first engine adapter

Status: Accepted; the adapter is pending S3 validation before it is coded.

## Context

The first engine must run against Azure OpenAI (GPT-5.4 and GPT-5.4-mini),
because that is the available model budget. Two facts constrain the choice.
Claude Code and the Claude Agent SDK are documented as Claude-only and do not
support routing to non-Claude models through a gateway. The OpenAI Codex CLI runs
against Azure OpenAI natively through its Responses API.

## Decision

The first `engine.Runner` implementation shells out to `codex exec` configured for
Azure. Model selection is per role (a larger model for planning and review, the
mini for mechanical fixes). Configuration lives in the Codex CLI's own config, so
the engine adapter mostly assembles a prompt, sets the working directory, and
parses the result. A Claude Code adapter is a later, parallel implementation of
the same interface for anyone who brings Claude models.

## Why not yet coded

The adapter's real work is prompt assembly and result parsing, and both depend on
how GPT-5.4 actually behaves inside a loop it was not tuned for. S3 is exactly
the test of that: revert known-good commits in a real repo and see whether the
Codex-driven loop reproduces them against the real gate, and at what token and
wall-clock cost. Writing the adapter, the prompts, and the budgets before that
evidence exists would bake in assumptions the spike is designed to check.

## Consequences

- The `Runner` interface already exists and is exercised by the fake, so adding
  this adapter is additive and low-risk once S3 reports.
- The engine is a config choice, not an architectural commitment. When the Azure
  credit runs out, changing the default is an adapter swap.
