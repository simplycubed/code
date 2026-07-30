# 0006. Runtime on GitHub Actions, and identity

Status: Accepted.

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

## Decision update: one App

Use one App: `simplycubed-code` carries the product's GitHub identity for
commits, pull requests, labels, and comments. Each job mints its own
installation token scoped to the current repository and only the permissions it
needs, preserving least privilege without adding a second principal for the
current loop.

Reason: the automated reviewer role defined in #32 is comment-only by design,
not a formal approval or request-changes reviewer. GitHub allows a COMMENT
review from the PR author's own identity; only APPROVE and REQUEST_CHANGES are
blocked. The current reviewer flow also passes findings in-process from the
reviewer role to the fixer prompt, not through a GitHub review round-trip, so a
separate App adds no capability today. Requiring adopters to install two Apps
for one tool would also be a worse product experience.

If a future automated reviewer needs to publish a formal review state rather
than comments, splitting identities remains an option to revisit. For the
present design, one App is the accepted decision.
