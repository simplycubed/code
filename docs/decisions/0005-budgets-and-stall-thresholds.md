# 0005. Budgets and stall thresholds

Status: Pending S3.

## Context

A loop needs limits: how many rounds before it gives up, how much wall-clock and
token cost per issue, and how it detects that it is stuck rather than making
progress. The loop already has the mechanisms. The numbers are the open question.

## What exists in code

- A per-run round cap (`Config.MaxRounds`, default 4) as a hard backstop.
- Stall detection by repetition: two consecutive gate failures with the same
  normalized signature end the run as a stall. The signature strips volatile
  detail (line numbers, durations) so "the same failure again" is recognized even
  when surface text differs.

## What is pending

- **The numbers.** The right round cap, the per-issue token and dollar ceiling,
  and the wall-clock budget per iteration are all functions of how many rounds a
  real model actually needs to clear a real gate. S3 produces that distribution.
  Setting them now would be guessing.
- **The budget hierarchy.** Per-issue envelope, per-role iteration caps, and a
  shared reviewer-fixer cycle cap (the one that actually bounds a review-and-fix
  ping-pong) are designed but not coded, for the same reason.
- **Escalation richness.** The loop escalates today with a comment and a label.
  The fuller form (an idempotent comment keyed by run id, a transcript summary
  with secrets redacted by absence) waits until there is a real transcript to
  redact.

## Consequences

Free model credits are a rate limit before they are a cost. S3 must record tokens
and wall-clock per issue so the budget can be set from data, and so multi-repo
fan-out can be checked against the real rate limit rather than assumed to fit.
