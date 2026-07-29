# 0004. Role prompts and the reviewer verdict

Status: Pending S3.

## Context

Two roles start the system: an implementer that edits and runs the gate, and a
read-only reviewer that judges a diff. The reviewer's output has to be machine-
gradable so the loop can act on it, and the roles' prompts have to reliably
produce the behavior the loop assumes.

## Direction (not yet coded)

- **Prompt shape.** Append to the engine's own system prompt rather than replace
  it, so the harness scaffolding the model relies on is preserved. Inject context
  by file path in the working tree (issue body, plan, diff, gate output) rather
  than by pasting it into the prompt, which keeps prompts cacheable and lets the
  model read only what it needs.
- **Exit conditions.** Each role's "done" is a concrete artifact the loop can
  check: for the implementer, the gate passes; for the reviewer, a schema-valid
  verdict file exists.
- **Verdict.** The reviewer writes a structured verdict (pass or fail, with an
  enumerated list of findings) as a file, which the loop validates against a
  schema. A malformed or missing verdict is treated as a gate failure that
  re-prompts, the same way a failing test would be. A verdict that claims pass
  while carrying a blocker finding is not trusted (`domain.Verdict.HasBlocker`
  already encodes this).
- **Reviewer posture.** Comment review only, never a GitHub approval (see ADR
  0002).

## Why pending

The exact prompt wording, the fields that survive being produced reliably by
GPT-5.4, and whether file-based structured output holds up in practice are all
things S3 measures. The in-process `Verdict` type exists; the wire schema, the
validation, and the prompts wait for evidence.
