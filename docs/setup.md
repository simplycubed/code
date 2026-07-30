# Setup

This is the adopter path that exists today: install the CLI, write the repo
config and caller workflow into your own repository, set the Azure values in the
right places, and let GitHub Actions drive issues to pull requests.

## Quickstart

Prerequisites:

- Go installed locally so you can run `go install`.
- `gh` authenticated against the repository you want to onboard.
- Write access on that repository.
- An Azure OpenAI endpoint and API key.

1. Install the CLI:

```sh
go install github.com/simplycubed/code/cmd/simplycubed@v0.1.2
simplycubed version
```

2. In the target repository, generate the starter files and labels:

```sh
simplycubed init --workflow
```

That writes `.github/simplycubed.yml`, writes `.github/workflows/simplycubed.yml`,
and creates the `sc:*` labels through your local `gh` auth. The files are local
changes in your repository; nothing is merged or installed remotely for you.

3. Edit `.github/simplycubed.yml` and set a real gate that is already green on
your default branch. A minimal example is:

```yaml
labelPrefix: sc
gate: make check
```

### Run it locally first

Before wiring Actions, you can prove the loop works from your terminal with the
same config file:

```sh
export AZURE_OPENAI_ENDPOINT="https://<resource>.openai.azure.com"
export AZURE_OPENAI_API_KEY="<key>"
simplycubed run owner/repo#N --repo-dir .
```

4. In the GitHub repository settings, add:

- Variable: `SIMPLYCUBED_GH_APP_ID`
- Secret: `SIMPLYCUBED_GH_APP_PRIVATE_KEY`
- Variable: `AZURE_OPENAI_ENDPOINT`
- Secret: `AZURE_OPENAI_API_KEY`

The Actions runtime authenticates as the `simplycubed-code` GitHub App, so the
App ID and private key are required. Create the App with `Contents`, `Issues`,
and `Pull requests` permissions only, webhook disabled, install visibility `Any
account`, then install it on the repository. Store the private key as the full
PEM contents, including the `-----BEGIN` and `-----END` lines.

Each job mints its own installation token for that repository, so the agent
authors commits, pull requests, and comments as `simplycubed-code[bot]`, and its
pull requests receive their own CI runs. A personal access token is not an
alternative: the reusable workflow accepts App credentials only.

5. Commit the generated config and workflow files in the target repository, open
a setup pull request, and merge it yourself. Setup files are written locally by
`simplycubed init` and merged by a human, because the runtime holds no
`workflows` permission and cannot add its own workflow files.

6. File an issue that describes a small change and apply the `sc:go` label.

7. Wait for the workflow to open a pull request. Review it like any other PR:

- Merge it yourself if it is good.
- Or request changes; the fixer loop will address feedback on the current head
  and push back to the same branch.

That is the first end-to-end path: issue -> PR -> human merge.

## Azure values

The shipped Codex-on-Azure setup needs these values:

| Name | Where it is used | Secret? |
| --- | --- | --- |
| `AZURE_OPENAI_ENDPOINT` | Base Azure OpenAI endpoint, for example `https://<resource>.openai.azure.com` | No |
| `AZURE_OPENAI_API_KEY` | Azure OpenAI API key | Yes |
| model or deployment name | Optional override passed as `--model` locally or `model:` in the caller workflow | No |

If you do not set a model override, the CLI and reusable workflow default to
`gpt-5.4`.

## Where the key lives

The API key lives in different places depending on where `simplycubed` runs.

- Local CLI run: export `AZURE_OPENAI_API_KEY` in the shell that runs
  `simplycubed`. Do not commit it, and do not expect a GitHub repository secret
  to appear in your local terminal.
- GitHub Actions run: store `AZURE_OPENAI_API_KEY` as a repository secret. The
  caller workflow passes it into the reusable workflow job as an environment
  variable.

In both cases the config references the key by environment variable name. The
key value does not belong in `.github/simplycubed.yml` or any committed file.

## Local CLI vs GitHub Actions

Use the same endpoint and key in both places, but wire them differently.

### Local CLI

For local runs such as `simplycubed run owner/repo#123`, the CLI reads:

- `AZURE_OPENAI_ENDPOINT` from your shell environment.
- `AZURE_OPENAI_API_KEY` from your shell environment.
- The repo gate and label prefix from `.github/simplycubed.yml`.

Example:

```sh
export AZURE_OPENAI_ENDPOINT="https://<resource>.openai.azure.com"
export AZURE_OPENAI_API_KEY="<key>"
simplycubed run owner/repo#123 --repo-dir .
```

### GitHub Actions

For the hosted-in-your-GitHub path, the caller workflow in your repository
passes:

- `vars.AZURE_OPENAI_ENDPOINT` to the reusable workflow input
  `azure-openai-endpoint`.
- `secrets.AZURE_OPENAI_API_KEY` to the reusable workflow secret
  `azure-openai-api-key`.
- `vars.SIMPLYCUBED_GH_APP_ID` to the reusable workflow input `github-app-id`.
- `secrets.SIMPLYCUBED_GH_APP_PRIVATE_KEY` to the reusable workflow secret
  `github-app-private-key`.

The reusable workflow installs the CLI, exports the endpoint and key for the job,
and runs `simplycubed run` or `simplycubed address`.

## Optional model override

If your Azure deployment name is not `gpt-5.4`, set it explicitly.

- Local CLI: pass `--model <deployment-name>`.
- GitHub Actions: uncomment and set `model:` in
  `.github/workflows/simplycubed.yml`.

The current shipped workflow uses one model value per run. Per-role model
tiering is not wired yet.
