# Changelog

All notable changes are recorded here and summarized again in the matching
GitHub release notes for each tag.

## v0.1.5

Supersedes v0.1.4, which was tagged from the wrong commit and is retracted in
`go.mod`. The content below is what v0.1.4 was meant to carry.

- Fixed the blocker that made every GitHub Actions run fail before doing any
  work: the CLI defaulted `--base` to `origin/HEAD`, which `actions/checkout`
  never creates. The worktree manager now resolves the remote's default branch,
  and the reusable workflow passes the base from the triggering event.

## v0.1.3

Supersedes v0.1.2, which was tagged from the wrong commit and is retracted in
`go.mod`. The content below is what v0.1.2 was meant to carry.

**Breaking (GitHub Actions runtime only):** the reusable workflow now
authenticates as the `simplycubed-code` GitHub App and no longer accepts the
`gh-token` personal-access-token secret. Callers moving from `v0.1.1` must
create and install the App, then supply `github-app-id` and
`github-app-private-key`. Callers pinned at `v0.1.1` are unaffected until they
bump. The local CLI path is unchanged and needs no App.

- GitHub App identity: each job mints its own installation token scoped to the
  current repository with contents, issues, and pull-requests permissions only,
  and verifies in the run log that it cannot reach Actions administration. The
  agent authors commits, pull requests, and comments as `simplycubed-code[bot]`,
  and its pull requests receive their own CI runs (which a `GITHUB_TOKEN`-
  authored pull request never does).
- ADR 0006's open one-App-versus-two question is decided: one App carries every
  role, with per-job token scoping for least privilege.
- Adopter docs rewritten around the shipped install path, including a
  local-CLI-first quickstart, and corrected to state plainly that the runtime
  holds no `workflows` permission and cannot add its own workflow files.
- Coverage reporting: the gate now runs tests with `-race` and writes a
  coverage profile, uploaded to Codecov from CI.

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
