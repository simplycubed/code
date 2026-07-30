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

## Decision update: two Apps, not one

Use two Apps: a worker App (`simplycubed-code`) that authors commits, pull
requests, labels, and comments, and a separate reviewer App for the future
automated review role.

Reason: a single principal cannot leave a "request changes" review on its own
pull request. Live runs hit exactly that wall when the CLI authenticated as the
repo owner, and the same GitHub author/reviewer restriction would still apply if
both roles shared one App identity. Two Apps preserve the separation GitHub
enforces at the principal level, while per-job installation tokens still provide
least privilege inside each role.

Today only the worker App is wired, because the human reviewer remains the
reviewing principal in the current loop. When the automated reviewer role is
connected, it must use the second App rather than the worker App's token.
