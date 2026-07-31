# Changelog

All notable changes are recorded here and summarized again in the matching
GitHub release notes for each tag.

## v0.1.8

- **The Actions runtime was dead in `v0.1.7` and is fixed here.** The comment
  dispatch fix gave the run step a second `env:` block instead of adding to the
  first. YAML keeps the last of two identical keys, so `GH_TOKEN` and both Azure
  variables were dropped, and GitHub, which is stricter than the parser the gate
  used, refused to load the file at all. Every run since that merge failed in
  about a second without starting a job. Upgrade from `v0.1.7`.
- The gate now rejects a duplicate YAML key anywhere in a workflow, template, or
  the embedded caller. `yaml.safe_load` accepted the broken file and kept the
  last key, which is exactly why the break reached a release: green gate, dead
  workflow. This is the second time a workflow defect passed a gate that only
  compiled Go, and the check is written to fail on the real file from `v0.1.7`.
- The App token step now uses `client-id`, which ends the deprecation warning
  on every run. The two inputs are not interchangeable in principle, but the
  action resolves `client-id || app-id` into one value, so adopters passing
  `github-app-id` keep working while the documented path moves to
  `SIMPLYCUBED_GH_APP_CLIENT_ID`.

## v0.1.7

- Fixed comment commands. The reusable workflow declared `comment-body` and never
  read it, so nothing called the parser: any comment beginning with the mention,
  from anyone with write access, started a full run. `@simplycubed-code help`
  started work instead of printing help, and a comment asking it not to proceed
  started the work anyway.
- Documentation caught up with the code. The README claimed the reviewer was not
  wired in and that the Claude adapter was planned, both shipped; `STATUS.md`
  named the wrong release and listed SHA-pinned actions as not done after they
  shipped. `preflight`, `--dry-run`, and the install self-test had no
  adopter-facing documentation at all and now do.
- New `docs/troubleshooting.md`, starting from the symptom every install-time
  failure shares: the run went green and nothing happened.
- Three diagrams: the credential split between the local CLI and Actions, the
  diagnosis tree for a silent success, and what triggers a run.

## v0.1.6

- **The Actions runtime works end to end.** Two environment blockers are fixed:
  the engine's sandbox could not start inside a runner, so the agent could not
  execute a single command; and a runner has no git identity, so commits failed
  after the gate had already passed.
- **Automated reviewer** (`review: true`, off by default). After the gate
  passes, a read-only reviewer judges the change and its findings go to the
  fixer before a human sees the pull request. It never approves and never
  merges. A pass carrying a blocker is not trusted.
- **Claude Code engine** (`engine: claude`, default `codex`). The loop, roles,
  and gate are unchanged by which model writes the code.
- **Comment commands**: `@simplycubed-code go`, `address`, and `help`, acted on
  only for commenters with write access.
- The reusable workflow carries values rather than decisions: authorization,
  identity resolution, and engine validation moved into the product, where they
  are tested. It is 265 lines shorter and its two jobs are now identical.
- Releases are cut by a workflow, not a local tag, and refuse to proceed when
  the changelog or any version pin disagrees with the version being released.
- Actions are pinned by commit SHA, with Dependabot keeping them current.
- The run ledger persists to an orphan branch, so a run's audit trail outlives
  the runner it happened on.
- An escalation now carries the engine's own account of the turn, which is what
  made the two blockers above diagnosable at all.

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
