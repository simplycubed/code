# SimplyCubed Code

An autonomous coding agent that lives inside your own GitHub. You file an issue, it opens a pull request, and a human decides whether to merge.

> Status: beta. The core issue-to-pull-request flow works end to end; expect rough edges, and there is no stable release yet. See [Status](#status).

## What it is

SimplyCubed Code turns a GitHub issue into a reviewed pull request without a person writing the code. A human files an issue describing the change. The agent implements it, a separate read-only reviewer role checks the work, a fixer addresses whatever the review turns up, and a pull request goes out for a human to merge.

It proposes. It does not dispose. The agent never pushes to `main` and never merges its own work. A person is always the one who clicks merge.

The part that makes it different from hosted coding agents is where it runs. Everything happens inside your own GitHub Actions, on your runners, using your minutes and your secrets. SimplyCubed hosts nothing, runs no server on your behalf, and never sees your code or your credentials. Compare that to tools like Copilot's coding agent, Devin, or Codex cloud, which run the model against your code on someone else's infrastructure.

## How it works

The loop is issue to pull request, driven entirely through GitHub.

1. A human files an issue and applies the `sc:go` label. That label is the only thing a person applies to start the work.
2. The agent picks up the issue, implements the change on a branch, and runs the repo's own quality gate.
3. A read-only reviewer role reviews the diff. A fixer role addresses the findings and runs the gate again.
4. Once the gate passes, the agent opens a pull request and hands it back to a human.
5. A human reviews and merges. The agent does not.

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
- Your model provider keys, your `GITHUB_TOKEN`, and any other secrets stay in your GitHub secret store. They are read by your own Actions runs and never transit our infrastructure.
- The agent holds no deploy credentials and has no path to production. The most it can do is open a pull request against a branch. A human and your branch protection rules decide what happens next.

The GitHub App identity is `simplycubed-code[bot]`. That bot is the single audit signal for everything the agent does.

## Getting started

Onboarding is meant to happen through GitHub itself rather than a local setup script.

1. Install the `simplycubed-code[bot]` GitHub App on the repo you want it to work in.
2. File a bootstrap issue.
3. The agent opens a set of setup pull requests that add its own configuration, the labels it uses, and the workflow files it needs.
4. Review and merge those, then file your first real issue and apply `sc:go`.

Because setup arrives as pull requests, you see exactly what the agent adds to your repo before any of it lands.

## Configuration

Configuration lives in `.github/simplycubed.yml`. A minimal file looks like this:

```yaml
labelPrefix: sc

gate: make check
```

The `gate` command is required. It is whatever your repo already runs to know a change is good, typically a typecheck, your tests, and a build, the same thing your CI runs. The agent cannot declare a change done until the gate passes.

A repo with no gate is refused, on purpose. An agent loop with nothing to stop it will wander, break things, and still report success. The gate is the safety mechanism, so the tool treats its absence as a configuration error rather than a default to paper over. The engineering that matters here is the gate, not the loop.

## Engines

The model that writes the code sits behind a pluggable `Runner` interface, so you bring your own provider.

The first engine adapter targets the Codex CLI running against Azure OpenAI (GPT-5.4 and 5.4-mini). A Claude Code adapter is planned. The `Runner` interface is the seam where other engines plug in.

## Status

Beta, and honest about it. The core issue-to-pull-request flow runs end to end (via the CLI, with the Codex-on-Azure engine), but there is no stable release yet and you should expect rough edges.

Roadmap, roughly in order:

- The core issue-to-pull-request loop with the reviewer and fixer roles.
- The self-onboarding flow via bootstrap issue and setup pull requests.
- The Codex on Azure OpenAI engine adapter.
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
