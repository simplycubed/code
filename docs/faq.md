# FAQ

Questions that come up when you point SimplyCubed Code at a repository for the
first time. Most of them are really one question in disguise: **what should the
gate be, and why won't the agent run until it is right?**

## Do I have to install the GitHub App to try it?

For the GitHub Actions runtime, yes. For local development, no.

You can use SimplyCubed Code two ways:

- as a local CLI (`simplycubed run` and `simplycubed address`) using your own
  `gh` authentication, which reads the repo config, works in a git worktree,
  runs your gate, and opens or updates a pull request as you;
- or through the reusable workflow that runs inside your own GitHub Actions, as
  described in the README's install section, which now mints a per-job GitHub
  App installation token for `simplycubed-code[bot]`.

The GitHub App is configured with only `contents`, `pull requests`, and
`issues`, and the reusable workflow proves that scope at runtime by expecting an
Actions-administration API call to fail.
That permission set also means the App cannot push edits under
`.github/workflows/`. When a change needs a workflow-file edit, make that commit
as a human or run the CLI locally under your own `gh` auth instead of the App.

## Why does it not use the engine's "dangerous" flags?

Both engines ship an escape hatch: Codex has `--dangerously-bypass-approvals-and-sandbox`
and a `danger-full-access` sandbox mode, and Claude Code has
`--dangerously-skip-permissions`. SimplyCubed Code does not set any of them for
you, and the reason is worth stating plainly because reaching for them is
tempting.

Both vendors put "dangerous" in the name deliberately. Codex's own help says its
bypass is "intended solely for running in environments that are externally
sandboxed". A GitHub Actions runner is ephemeral, which protects the machine,
but the things worth protecting are inside it: the model provider key, and
whatever else the job holds. Ephemerality does not undo an exfiltrated secret.

The distinction that decides it is between a person running the CLI at their own
desk and an unattended Action running a model. In the first case a human is
present, watching, and can interrupt. In the second there is nobody, the trigger
is an issue body that anyone with access to the tracker can write, and the run
happens whether or not it is going well. That is the case that needs *more*
constraint, not less, and a product that sells human-in-the-loop should not
disable a safety control on the operator's behalf to make an unattended run
convenient.

So when a change cannot be made under the constraints the runtime accepts, the
run stops and a human does it. That is not a gap in the design; it is the
design. The same rule already applies to workflow files, which the App
deliberately cannot push.

`SIMPLYCUBED_SANDBOX` exists as a knob so an adopter who has genuinely
externally sandboxed their runners can widen it themselves, with their eyes
open. Nothing in this repository sets it.

## Are the `sc:` labels created for me?

Not automatically, yet. For now the labels are created once when you onboard a
repo. The planned self-onboarding flow will propose them (and the config and
workflow files) as a pull request you merge, so nothing is created behind your
back. Until then, creating the six state labels is a one-time setup step.

## The agent won't propose anything. Why?

Almost always because the gate does not pass, and the gate is the whole point.
SimplyCubed Code will not declare a change done, or open a pull request, until the
gate command exits zero. A loop with nothing to stop it wanders, breaks things,
and still reports success, so a repo with no gate is refused and a repo whose gate
cannot pass gets a change that stalls and asks for a human.

Three things trip up a first run, all of them about the gate.

## 1. Your gate has to be green on your own `main` first

If your repo's own checks fail on a clean checkout, the agent inherits that
failure and can never reach a green gate. This is more common than it sounds. A
real example from onboarding a repo: `make fmt-check` failed on `main` because
seventeen files had drifted out of `gofmt` formatting. CI had never caught it,
because the lint job ran a linter with no formatter enabled and the format check
was never wired into CI. The formatting slowly rotted while every check stayed
green.

The fix is not to weaken the gate to route around the breakage. It is to fix the
baseline (here, one `gofmt -w`) and then enforce it so it cannot drift again (here,
enabling the `gofmt` formatter in the existing linter, which CI already runs).
Now the gate is stable and the same check protects your humans too.

The lesson generalizes: **onboarding is a good time to find out your baseline is
not as green as you thought.** Fix the baseline, do not lower the bar.

## 2. Your gate should mirror your real CI, not a weaker subset

If the gate is weaker than what your CI actually enforces, the agent will produce
a change that passes the gate and then fails your CI. That is the worst outcome:
it looks done and is not.

The first live run on a repo made exactly this mistake. The configured gate was a
plain build plus a slice of the tests, while the repo's CI also ran a linter and
built across several language versions. The agent opened a clean-looking pull
request that then failed CI on lint and on a version bump the build could not
take. The change was fine; the gate was lying.

So set the gate to what your CI gates. If your CI runs a formatter, a linter, a
vet step, a build, and tests, your gate should run those too.

## 3. Some checks belong in CI, not in the gate

The gate runs on one machine, in a worktree, before a pull request exists. A few
kinds of check cannot run there and should stay in CI, which runs after a human
merges:

- **A build or test matrix across several language or runtime versions.** The
  gate builds once, with the toolchain on the machine.
- **Container image builds** and anything that needs Docker.
- **Tests that need an external service** (a real database, a running server, a
  cloud emulator).

The distinction that matters for tests is whether they are self-contained. Tests
that spin up an in-process server (for example Go's `httptest`) run fine in the
gate. Tests that expect a service to already be listening on a port do not, so
leave those to CI.

The rule of thumb: **put everything runnable on one machine in the gate, and
leave the genuinely environmental checks to CI.** A faithful single-machine
subset is the goal, not a copy of every CI job.

## So what should my `gate:` actually be?

Prefer the commands your repo already uses to know a change is good. If you have a
`make check` or an `npm run check` that runs standalone, use it. Otherwise compose
the runnable checks with `&&` so the first failure stops the gate:

```yaml
gate: <vet> && <lint> && <build> && <tests>
```

A concrete Go example, after making sure each part is green on `main`:

```yaml
gate: go vet ./... && golangci-lint run ./... && go build ./... && go test ./...
```

Verify it passes on a clean checkout before you rely on it. If it does not, that
is a baseline problem to fix (see question 1), not a reason to trim the gate.

## Does the agent ever change my gate or my tests to make things pass?

No. It is told, in every turn, never to edit the gate command, its configuration,
the CI workflow files, or any test in order to get a green result, and to stop and
explain instead of forcing one. That rule is also enforced outside the prompt,
because a model will otherwise route around it. If a change genuinely cannot pass
the gate legitimately, the run stops and asks for a human rather than pushing
through.

## What happens when it cannot finish?

It stops and hands the issue or pull request back to you with a note, rather than
opening or updating anything. The strongest action it can take is a pull request
against a branch. It never merges, and it holds no deploy or production
credentials.
