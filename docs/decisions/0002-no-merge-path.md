# 0002. No merge path in the binary

Status: Accepted, implemented.

## Context

The product's core promise is that it proposes changes but a human disposes of
them: the agent proposes, a human disposes. A tool that can merge its own work,
or that has the capability sitting one flag away, cannot make that promise
credibly, and a security reviewer evaluating whether to grant it repo access
will look for exactly this. The promise is independent of which model backs the
engine.

## Decision

There is no merge operation anywhere in the system. The `forge.Forge` interface
has `OpenPR`, `SetState`, and `Comment`, and no `Merge`. The loop's only success
side effect is opening a pull request. The reviewer role, when it exists, will
post a comment review, never a GitHub approval, because a bot approving its own
author's pull request is not a real gate.

## Consequences

- The strongest thing the system can do is open a pull request. Merge is a human
  action, backed by the adopter's own branch protection.
- This is enforced structurally (the capability is absent), not by policy or
  prompt. There is no flag to add it later without a deliberate code change and
  review.
- The agent identity holds no deploy or production credential. It cannot reach
  production even if it misbehaves.
