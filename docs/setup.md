# Setup: pointing the engine at a model

The engine needs a few values to talk to a model provider. The first engine
adapter targets the OpenAI Codex CLI against Azure OpenAI (see
[ADR 0003](decisions/0003-codex-azure-first-engine.md)). Wiring these values into
the code waits on the S3 replay spike, so for now this is a reference for running
that spike rather than a config path the product reads on its own.

## What the Azure engine needs

Three non-secret values and one secret.

| Input | What it is | Secret? |
| --- | --- | --- |
| Resource name | The `<name>` in `https://<name>.openai.azure.com` | No |
| GPT-5.4 deployment name | The deployment you created for the full model (the name is yours, not necessarily `gpt-5.4`) | No |
| GPT-5.4-mini deployment name | The deployment for the small model | No |
| `AZURE_OPENAI_API_KEY` | The resource key | Yes |

The Codex CLI is configured for Azure like this (base URL includes `/openai/v1`,
key read from the environment by name, Responses wire API):

```toml
model = "<gpt-5.4 deployment name>"
model_provider = "azure"

[model_providers.azure]
name = "Azure OpenAI"
base_url = "https://<resource>.openai.azure.com/openai/v1"
env_key = "AZURE_OPENAI_API_KEY"
wire_api = "responses"
```

## Where the key must live depends on where the engine runs

This distinction matters and is easy to get wrong.

- **In production (GitHub Actions runtime):** the key belongs in a repository or
  environment **secret** named `AZURE_OPENAI_API_KEY`. A workflow reads it and
  passes it to the job. This is the right home for the deployed product.
- **For a local run or the validation spike:** a repository secret is **not**
  visible to a local shell. The key must be in the local environment where
  `codex` runs. Keep it out of your shell profile and out of any repo: put it in
  a local, git-ignored env file and source it only for the run.

In both cases the key is referenced by name; the value never goes in a config
file or the repository.

## Model tiering

Larger model for planning and review, mini for mechanical fixes. Per-role model
selection is part of the engine adapter, which is pending S3.
