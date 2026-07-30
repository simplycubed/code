# SimplyCubed Code

An autonomous coding agent that lives inside your own GitHub. You file an issue, it opens a pull request, and a human decides whether to merge.

> Status: beta. `v0.1.1` is the latest release; expect rough edges. See [Status](#status).

## What it is

SimplyCubed Code turns a GitHub issue into a pull request without a person writing the code. A human files an issue describing the change; the agent implements it against the repo's own quality gate and opens a pull request for a human to merge. When a human then requests changes on that pull request, a fixer role reads the feedback, addresses it, re-runs the gate, and pushes back to the same branch for another look.

It proposes. It does not dispose. The agent never pushes to `main` and never merges its own work. A person is always the one who clicks merge.

The part that makes it different from hosted coding agents is where it runs. Everything happens inside your own GitHub Actions, on your runners, using your minutes and your secrets. SimplyCubed hosts nothing, runs no server on your behalf, and never sees your code or your credentials. Compare that to tools like Copilot's coding agent, Devin, or Codex cloud, which run the model against your code on someone else's infrastructure.

## How it works

The loop is issue to pull request, driven entirely through GitHub.

1. A human files an issue and applies the `sc:go` label. That label is the only thing a person applies to start the work.
2. The agent implements the change on a branch and runs the repo's own quality gate, using the gate's output to guide each retry until it passes or it stops and asks for a human.
3. Once the gate passes, the agent opens a pull request and hands it back to a human.
4. A human reviews. If they request changes, a fixer role reads the feedback, makes the changes, re-runs the gate, and pushes back to the same pull request for another look. Only feedback left against the current head is addressed, so the loop never re-litigates a comment it already handled.
5. A human merges. The agent does not.

A separate read-only reviewer role is defined in the code but is not yet wired into the loop, so today the review in step 4 is the human's. The roadmap below tracks it.

### Label lifecycle

You drive the agent by applying one label and reading the pull request. The bot manages the rest of the state. Labels use a configurable prefix (`labelPrefix`, default `sc`). An issue sits in one state at a time.

| Label | Applied by | Meaning |
| --- | --- | --- |
| `sc:go` | human | Start work on this issue. The only trigger a person sets. |
| `sc:queued` | bot | Accepted, waiting to start. |
| `sc:working` | bot | Implementing the change. |
| `sc:review` | bot | Review and fix pass in progress. |
| `sc:blocked` | bot | Needs a human. The agent stopped and left a note. |
| `sc:done` | bot | Pull request merged. |

## Running in your own GitHub

The whole thing runs on GitHub Actions, event-driven, with no server and no VM for SimplyCubed to operate. When you install the app and file issues, the work executes on your runners inside your organization.

What this buys you:

- Your code stays in your repos. SimplyCubed never receives it.
- Your model provider keys, the GitHub App's private key, and any other secrets stay in your GitHub secret store. They are read by your own Actions runs and never transit our infrastructure.
- The agent holds no deploy credentials and has no path to production. The most it can do is open a pull request against a branch. A human and your branch protection rules decide what happens next.

The GitHub App identity is `simplycubed-code[bot]`. That bot is the single audit signal for everything the agent does.

## Getting started

The current onboarding path is one workflow change plus one local init step.
Use [Install into your GitHub](#install-into-your-github): copy the caller
workflow, add the Azure endpoint and key, run `simplycubed init`, merge, then
label an issue `sc:go`.

## Installation

Install the pinned release you want to run:

```sh
go install github.com/simplycubed/code/cmd/simplycubed@v0.1.1
simplycubed version
```

That prints `0.1.1`. Pre-1.0 releases follow semver with the usual caveat: minor
versions may still change behavior. Pin the tag you have validated rather than
floating on `@latest`.

## Install into your GitHub

To run the loop inside your own GitHub Actions:

1. Create and install the GitHub App, `simplycubed-code`.
   Repository permissions: `Contents`, `Pull requests`, and `Issues` only.
   Do not grant `Workflows`, `Administration`, `Environments`, or `Secrets`.
   Disable the App webhook.
   Set install visibility to `Any account`.
   Install it on the repo.
2. Copy [`docs/templates/simplycubed-caller.yml`](docs/templates/simplycubed-caller.yml) into the adopter repo as `.github/workflows/simplycubed.yml`, then merge that one workflow pull request. The template pins a released reusable workflow tag (`v0.1.1` today), not `main`.
3. Add repository variable `SIMPLYCUBED_GH_APP_ID`, repository secret `SIMPLYCUBED_GH_APP_PRIVATE_KEY`, repository variable `AZURE_OPENAI_ENDPOINT`, and repository secret `AZURE_OPENAI_API_KEY`.
   The private key secret must be the full PEM contents, including the `-----BEGIN` and `-----END` lines.
4. Run `simplycubed init` once in the adopter repo so `.github/simplycubed.yml` and the `sc:*` labels exist, then merge that setup change.
5. File an issue and apply the `sc:go` label. Reviews submitted on the resulting pull request call back into the same reusable workflow for the fix-on-request loop.

Each reusable-workflow job mints its own installation token for the current
repository and asks only for `contents`, `pull requests`, and `issues`. The
workflow then probes an Actions-administration endpoint and expects a denial, so
the run log shows the token does not carry the workflow/admin scope the App was
deliberately denied.

## Configuration

Configuration lives in `.github/simplycubed.yml`. A minimal file looks like this:

```yaml
labelPrefix: sc

gate: make check
```

The `gate` command is required. It is whatever your repo already runs to know a change is good, typically a typecheck, your tests, and a build, the same thing your CI runs. The agent cannot declare a change done until the gate passes.

By default the commits and pull requests the agent generates carry a "SimplyCubed Code" marker: a `Co-Authored-By` trailer on the commit and a footer line on the pull-request body, the same convention Claude Code uses for its own commits. To turn it off, set `attribution: false`:

```yaml
gate: make check

attribution: false
```

Generated pull requests carry a one-line body by default. Set `prDescription: rich` to have the agent add a generated walkthrough, a changes table, and a mermaid sequence diagram to the body of each pull request it opens. The sections are marked as generated and are additive to your own review, never a substitute for it; if the generation fails or produces nothing valid, the pull request opens with the plain body.

```yaml
gate: make check

prDescription: rich
```

A repo with no gate is refused, on purpose. An agent loop with nothing to stop it will wander, break things, and still report success. The gate is the safety mechanism, so the tool treats its absence as a configuration error rather than a default to paper over. The engineering that matters here is the gate, not the loop.

Getting the gate right is where first runs stall: it has to be green on your own `main`, it should mirror what your CI actually enforces, and the genuinely environmental checks (a version matrix, Docker, tests that need a running service) stay in CI. The [FAQ](docs/faq.md) walks through each of these with real onboarding examples.

## Engines

The model that writes the code sits behind a pluggable `Runner` interface, so you bring your own provider.

The first engine adapter targets the Codex CLI running against Azure OpenAI (GPT-5.4 and 5.4-mini). A Claude Code adapter is planned. The `Runner` interface is the seam where other engines plug in.

## Status

Beta, and honest about it. Two loops run end to end via the CLI on the Codex-on-Azure engine: issue to pull request, and fix-on-request (a human requests changes, the fixer addresses them and pushes back). `v0.1.1` is the latest release and you should still expect rough edges.

Roadmap, roughly in order:

- The issue-to-pull-request loop. **Done.**
- The fix-on-request loop: a human requests changes, a fixer role addresses them. **Done.**
- Wiring the read-only reviewer role into the loop so a diff is reviewed before it reaches a human.
- The self-onboarding flow via bootstrap issue and setup pull requests.
- The Codex on Azure OpenAI engine adapter. **Done.**
- The Claude Code engine adapter.

If you are evaluating this for real work today, the honest answer is to watch the repo and check back. It is not ready.

## Contributing

The source is open under Apache-2.0, so you are free to read, audit, use, and fork it. SimplyCubed Code is developed by SimplyCubed and is not currently accepting external code contributions or pull requests. Bug reports and security reports are welcome through [issues](https://github.com/simplycubed/code/issues) and the process in [SECURITY.md](SECURITY.md). See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

## Inspiration and lineage

SimplyCubed Code is a clean rebuild inspired by the open-source project [nexu-io/looper](https://github.com/nexu-io/looper). No code was copied.

## Security

The agent proposes changes and never merges them, and it holds no deploy or production credentials. The strongest action available to it is opening a pull request against a branch, which a human then reviews.

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## About SimplyCubed

SimplyCubed builds AI automation for teams that would rather not hire for it. More at [simplycubed.com](https://simplycubed.com).

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE).

"SimplyCubed" and "SimplyCubed Code" are trademarks of SimplyCubed. The license does not grant any right to use these names. See [TRADEMARK.md](TRADEMARK.md).
