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

Provider rate limits constrain before per-token cost does. S3 must record tokens
and wall-clock per issue so the budget can be set from data, and so multi-repo
fan-out can be checked against the real rate limit rather than assumed to fit.

## Measured (S3-B, 2026-07-30)

First real numbers, from a single-file logic fix on a small Go repo graded by
`make check`:

- About 34k tokens and roughly two and a half minutes for one fix. Treat that as
  a floor, not an average: it is a small repo and a one-line bug. Real issues
  will cost more.
- The model deployments used carry large per-minute token quotas (on the order of
  500k and 1M tokens per minute). Rate limit is not the near-term constraint for
  single-repo or small multi-repo use; per-issue cost is.
- Those deployments were set to auto-upgrade to each new default model version.
  That is a model-drift risk. Pin a specific version and canary a bump against
  the golden-issue suite rather than inheriting silent upgrades.

The stall and budget mechanisms already exist in code; these are the first data
points toward setting the actual thresholds.
