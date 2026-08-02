# Changelog

All notable changes are recorded here and summarized again in the matching
GitHub release notes for each tag.

## v0.2.0

**This release makes the product installable by someone who is not us.** Every
change below comes from one root cause: it was built by the account that owns
the GitHub App, so the vendor case and the adopter case had never been
separated. Four of the defects were only reachable by an adopter, which is why
none of them had been seen.

**Breaking.** The command prefix and two configuration names change. There are
no known installations other than this repository, so no migration path is
provided; set the new names and regenerate the caller workflow.

- **You create your own GitHub App, and `init` now explains how.** The setup
  instructions told every adopter to create an App named `simplycubed-code`.
  App names are globally unique and we hold that one, so the step was only
  performable by the account that already owns it; everyone else got "Name has
  already been taken". Neither `init` nor the docs said why the App must be
  theirs: the private key is what mints tokens against their repository, so a
  shared key would let its holder act on every other installation. That single
  missing sentence is what made "create a GitHub App" read as busywork we could
  have done for them. `init` now prints the creation URL, resolved to the
  organisation form when the repository belongs to one, the three permissions
  with their access levels, webhook off and why, install visibility and why it
  is the step most often missed, and where the `Iv23` Client ID and the
  download-once private key come from.
- **Comment commands are `/simplycubed`, not `@simplycubed-code`.** An
  @-mention of our App cannot be right in anyone else's repository: their bot
  carries a different login, so the trigger matched nothing, and because our App
  is public the handle rendered there as a mention of an account they had never
  installed. The parser held the same literal, so templating the workflow
  trigger alone would not have helped. A prefix that is not a handle needs no
  templating and keeps one static caller workflow.
- **Workflow-push restriction is decided by identity, not by our App's name.**
  The check compared the authenticated login against a hardcoded
  `simplycubed-code[bot]`, so for every adopter it was false and the pre-flight
  never ran. They learned about the refusal at push time instead, after a full
  loop and its model spend. Any `[bot]` login is now restricted; an empty login
  stays unrestricted, because that is a human running locally and a human can
  push workflow files.
- **Escalations name the files that caused them.** "the change touches
  `.github/workflows/`" left a reader with nothing but the agent's own account
  of what it had edited, which is not evidence. Working out whether one real
  escalation was correct cost two investigation rounds and produced a wrong
  diagnosis. The message now lists the paths git actually reported.
- **The gate no longer stamps VCS metadata into binaries it throws away.**
  `go build` failed in the loop's worktree with `error obtaining VCS status:
  exit status 128` before any repository code compiled, so every agent run
  reported a red gate for a reason unrelated to the change under test. A gate
  that cannot run where the loop runs is not a gate.
- **One naming scheme for the four values an adopter sets.** Two carried the
  `SIMPLYCUBED_` prefix and two did not, so on an organisation settings page the
  four did not read as one tool's configuration and nobody auditing later could
  tell what `AZURE_OPENAI_API_KEY` belonged to. The unprefixed pair was also the
  conventional name for that credential, so an organisation already using Azure
  OpenAI either hit a conflict or silently shared one key between this runtime
  and something unrelated.

  | Section | Name |
  | --- | --- |
  | Variables | `SIMPLYCUBED_GH_APP_CLIENT_ID` |
  | Secrets | `SIMPLYCUBED_GH_APP_PRIVATE_KEY` |
  | Variables | `SIMPLYCUBED_AZURE_OPENAI_ENDPOINT` (was `AZURE_OPENAI_ENDPOINT`) |
  | Secrets | `SIMPLYCUBED_AZURE_OPENAI_API_KEY` (was `AZURE_OPENAI_API_KEY`) |

  `SIMPLYCUBED_SELF_LOGIN` becomes `SIMPLYCUBED_GH_APP_LOGIN`; it parsed as
  "SimplyCubed's login" when it means the bot identity the run holds.
- **`preflight` checks those four and names the section a missing one belongs
  in.** Nothing checked them until a real run failed, and the one thing that did
  was deleted at the end of install: the self-test names the section, then
  `init` tells the adopter to remove it. Variables and Secrets are different
  tabs, and a value filed under the wrong one reads back as empty rather than
  failing, which happened during a real install and was found by reading the
  caller workflow. A configuration miss now exits `3`, so a caller can tell "go
  and set this" from a bug without matching on message text.
- **Removed the `github-app-id` workflow input.** It survived as a fallback for
  callers that do not exist, and it invited the wrong credential: the name says
  App *ID*, the numeric one, while the token action needs the `Iv23` client ID.

## v0.1.9

- **The GitHub Actions runtime does the work now.** Until this release the
  issue-to-PR loop had never once produced a pull request from Actions, on this
  repository or any other. The engine confines itself with a bundled bubblewrap,
  bubblewrap builds its sandbox from an unprivileged user namespace, and Ubuntu
  24.04 forbids those by AppArmor policy, so the sandbox died at startup on
  every run:

  ```
  bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted
  ```

  Every command the engine tried then failed, including `pwd`, so it read
  nothing and changed nothing while still exiting successfully. That is why this
  presented for so long as an agent quietly declining to work rather than one
  that had never been able to start. The workflow now enables the kernel feature
  the sandbox is built from, which reads like a weakening and is the reverse:
  with the restriction in place the engine was confined by nothing, because its
  confinement never loaded. Measured on a runner in both directions, same
  binary, only that setting changing. None of the engines' own "dangerous" flags
  are set, here or anywhere.
- Comment commands answer on the thread instead of in a log nobody opens.
  `@simplycubed-code help` produced the right text and printed it inside the
  runner, so the person who asked saw silence. Worse, `address` on an issue ran
  anyway and failed two layers down with a raw GraphQL error, because nothing
  checked that a pull-request verb had been aimed at a pull request. The agent
  now resolves which surface a comment arrived on, answers with the verb that
  does apply, and stays green: someone using the wrong word is not a failure. A
  comment that merely mentions the agent in passing still says nothing at all.
- `engine: claude` no longer demands Azure credentials it never uses. The
  adapter was selectable and unusable, because the CLI required an Azure
  endpoint and key whatever engine was chosen, and then wrote a Codex provider
  config the Claude path never reads. Both are now conditional on the engine.
  This is the local CLI path; the reusable workflow still installs only the
  Codex CLI, which is tracked separately.
- The repository's own comment job installs the commit under test rather than
  the last release, matching the two jobs beside it. It was the one path whose
  regressions could not be caught before they shipped.

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
