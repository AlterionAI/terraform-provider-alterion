# terraform-provider-alterion

A [Terraform](https://terraform.io) provider for [Alterion](https://alterion.ai)
Orion — the control plane for AI agents. It lets you register and manage
"asserted agents" in Orion, and resolve an agent's gateway path key ahead of
its runtime existing, from Terraform.

Built with [`terraform-plugin-framework`](https://github.com/hashicorp/terraform-plugin-framework)
on the [scaffolding-framework](https://github.com/hashicorp/terraform-provider-scaffolding-framework)
layout.

## What it talks to

Orion web app REST routes under `/api/v1/agents/asserted/*` (bearer token
auth, `Authorization: Bearer orion_at_<hex>`). This provider is a thin client
over that API — it does not talk to any cloud provider directly.

## Provider block

```hcl
terraform {
  required_providers {
    alterion = {
      source = "alterion/alterion"
    }
  }
}

provider "alterion" {
  orion_url = "https://orion.example.com"  # or ALTERION_ORION_URL
  api_token = var.alterion_api_token       # or ALTERION_API_TOKEN (recommended: use the env var, not a committed value)

  # Optional. When set, the alterion_agent_path_key data source computes
  # gateway_base_url locally (gateway_url + "/" + path_prefix + "/" + short_id)
  # if the server response's gatewayBaseUrl was null.
  gateway_url = "https://gw.example.com"
}
```

## The two building blocks

### `data "alterion_agent_path_key"`

Resolves an agent's deterministic gateway path key from `environment` +
`slug` alone — no agent needs to exist yet. Use this to compute a runtime's
outbound gateway URL *before* creating the runtime.

```hcl
data "alterion_agent_path_key" "this" {
  environment = "production"
  slug        = "123456789012-us-east-1-support-bot"
}

output "gateway_base_url" {
  value = data.alterion_agent_path_key.this.gateway_base_url
}
```

### `resource "alterion_agent"`

Registers (creates/updates/deletes) the asserted agent row itself. `environment`
and `slug` force replacement if changed; every other attribute updates in
place via a re-`POST` (the server upserts on `environment` + `slug`).

```hcl
resource "alterion_agent" "this" {
  environment  = "production"
  slug         = "123456789012-us-east-1-support-bot"
  display_name = "Support Bot"
  runtime_arn  = aws_bedrockagentcore_agent_runtime.this.agent_runtime_arn
}
```

`terraform import` is **not supported** — the Orion API has no GET-by-
short-id route to reconstruct `environment`/`slug`/`display_name` from a
short id alone. Recreate the resource in configuration instead.

## Ordering: compute-early, register-late

Putting the two together is the point of this provider. See
[`examples/agentcore`](examples/agentcore) for a full AWS Bedrock AgentCore
example and the reasoning behind the ordering:

```
1. data.alterion_agent_path_key   →  gateway_base_url (deterministic, no agent needed yet)
2. aws_bedrockagentcore_agent_runtime  →  created with gateway_base_url baked into its env vars
3. alterion_agent                 →  registered now that runtime_arn exists, depends_on the runtime
```

## Local development

Requires Go 1.27+ and Terraform.

```bash
go build -o terraform-provider-alterion .
go vet ./...
gofmt -l .          # must print nothing
go test ./...       # unit tests (internal/client) — no network access
TF_ACC=1 go test ./... -run TestAcc -v   # + acceptance-style tests (internal/provider), against an in-process fake Orion server, not a real deployment
```

### Using a local build with Terraform (dev overrides)

Add to `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides {
    "alterion/alterion" = "/absolute/path/to/this/repo"
  }
  direct {}
}
```

Then `go build -o terraform-provider-alterion .` in this repo before running
`terraform plan`/`apply` in a consumer configuration — Terraform will use the
locally built binary instead of fetching from the registry, and will print a
warning reminding you dev overrides are active.

## Docs

Generated with [`tfplugindocs`](https://github.com/hashicorp/terraform-plugin-docs)
into [`docs/`](docs). Regenerate after schema changes:

```bash
tfplugindocs generate --provider-name alterion
```

## License

[MPL-2.0](LICENSE), consistent with HashiCorp's own providers.
