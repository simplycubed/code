# Changelog

All notable changes are recorded here and summarized again in the matching
GitHub release notes for each tag.

## v0.1.1

- Reusable GitHub Actions workflow: run the issue-to-PR and fix-on-request
  loops inside an adopter's own Actions, with labeler/reviewer authorization,
  per-item concurrency, a self-review skip, and a configurable engine model.
- `simplycubed init --workflow` writes the version-pinned Actions caller
  workflow, so installing into a repo is one merged PR plus one secret.
- Release-on-tag workflow: pushing a `v*` tag re-runs the gate, enforces the
  CHANGELOG and stamped-version checks, and publishes the GitHub release.
- Generated pull-request descriptions (`prDescription: rich`): walkthrough,
  changes table, and sequence diagram on PRs the agent opens.
- CLI hardening: flags are honored before or after positional arguments, and
  engine scratch (`.gopath` module cache) can no longer leak into commits.

## v0.1.0

- First tagged release.
- Added runtime version stamping so `go install ...@v0.1.0` reports `0.1.0` in
  `simplycubed version`.
- Documented the pinned install form and the minimal release process.
