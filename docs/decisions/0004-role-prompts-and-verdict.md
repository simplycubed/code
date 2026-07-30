# 0004. Role prompts and the reviewer verdict

Status: Accepted. The reviewer is wired into the loop; see the note at the end for what shipped and what changed.

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

## What shipped

The reviewer runs after the gate passes and before the pull request opens. It
writes a verdict to `.simplycubed/verdict.json`, the same scratch-directory
transport the describer uses, so it cannot leak into a commit.

Two details differ from the direction above, both learned rather than designed:

- **A malformed verdict does not re-prompt.** It is treated as an absent
  judgment and the change goes to the human as it stands. Re-prompting on a
  parse failure risks a loop that spends rounds arguing with itself, and the
  fallback a human already provides is cheaper.
- **A verdict is not trusted merely because it says pass.** `Trusted` requires
  a pass with no blocker, so a reviewer cannot wave through a change it has
  just called unmergeable. That contradiction is resolved pessimistically.

Unknown severities are rejected rather than downgraded: a finding the loop
cannot rank is one it cannot act on correctly.
