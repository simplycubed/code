# 0006. Runtime on GitHub Actions, and identity

Status: Accepted for the runtime; the one-App-versus-two identity question is
open.

## Context

The product must run without SimplyCubed hosting anything, and it must act in a
repository under a bot identity whose permissions are least-privilege.

## Decision: runtime

The system runs entirely inside the adopter's GitHub Actions. One role turn is
one Actions job, triggered by an issue label or a review event and chained
through the events each turn emits. State lives in labels, pull requests, and a
JSONL ledger on an orphan branch. There is no server, no database, and no VM for
SimplyCubed to operate. The product ships as a published Action plus reusable
workflows.

The Action packaging itself (the `action.yml`, the container image, the reusable
workflow) is not yet coded; it is straightforward once the loop and the engine
adapter exist, and it is easier to get right against a working loop than to
scaffold in advance.

## Decision: identity

The bot is a GitHub App. Its installation token is minted per job and scoped to
contents plus pull requests plus issues, and nothing else. It never receives
permission over workflows, administration, environments, or secrets, so it cannot
edit its own CI gate or reach deploy configuration.

## Open: one App or two

A single App can carry both the implementer and reviewer roles, with the role
shown in the comment header and least privilege achieved by scoping the
per-request token. Two Apps (a worker identity and a reviewer identity) buy
identity-level audit separation at the cost of a second install and a second key.
Per-request token scoping is required either way. This is deferred to when the
Action is built; the loop does not depend on the answer.
