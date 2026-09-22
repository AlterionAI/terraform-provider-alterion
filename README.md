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

Both resources use the same **cloud-agnostic identity**: `environment` +
`cloud_provider` (one of `aws`, `gcp`, `azure`; defaults to `aws`) +
`cloud_account_id` (an AWS account id, GCP project id, or Azure subscription
id/GUID, matching `cloud_provider`) + `cloud_region` + `workload_name`. None
of this is tied to any one cloud service — see
[`examples/agentcore`](examples/agentcore) (AWS Bedrock AgentCore) and
[`examples/ecs`](examples/ecs) (a plain AWS ECS service) for two different
workload types registered through the identical resource shape.

### `data "alterion_agent_path_key"`

Resolves an agent's deterministic gateway path key from the identity alone
— no agent needs to exist yet. Use this to compute a runtime's outbound
gateway URL *before* creating the runtime.

```hcl
data "alterion_agent_path_key" "this" {
  environment      = "production"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "support-bot"
}

output "gateway_base_url" {
  value = data.alterion_agent_path_key.this.gateway_base_url
}
```

### `resource "alterion_agent"`

Registers (creates/updates/deletes) the asserted agent row itself. The
identity attributes (`environment`, `cloud_provider`, `cloud_account_id`,
`cloud_region`, `workload_name`) force replacement if changed; every other
attribute updates in place via a re-`POST` (the server upserts on
`environment` + the identity).

```hcl
resource "alterion_agent" "this" {
  environment          = "production"
  cloud_account_id     = "123456789012"
  cloud_region         = "us-east-1"
  workload_name        = aws_bedrockagentcore_agent_runtime.this.agent_runtime_name
  workload_resource_id = aws_bedrockagentcore_agent_runtime.this.agent_runtime_arn
  workload_type        = "bedrock-agentcore-runtime"
}
```

`workload_resource_id` (the workload's own post-create unique id: an AWS
ARN, a GCP full resource name, or an Azure resource id) and `workload_type`
are both optional; when `workload_type` is omitted the server infers it
from `workload_resource_id`. `display_name` is optional and defaults to
`workload_name` when omitted.

`terraform import` is **not supported** — the Orion API has no GET-by-
short-id route to reconstruct `environment`/`cloud_provider`/
`cloud_account_id`/`cloud_region`/`workload_name`/`display_name` from a
short id alone. Recreate the resource in configuration instead.

## Ownership and `adopt`

An agent's owner is the **Orion user who minted the automation token**
(`api_token`) used to register it — recorded server-side as
`registered_by = automation:user:<email>` — not the token itself. That
means:

- **Rotating the token doesn't change ownership.** Swap `ALTERION_API_TOKEN`
  for a freshly minted token from the same user and `terraform apply` keeps
  working against agents that token's predecessor registered.
- **Tokens expire** (90 days by default) and stop working if the minting
  user loses the approver role. The provider surfaces that as a `401` —
  "token rejected; mint a new one" — not a permission error on the agent
  itself.
- **`adopt = true`** (on `alterion_agent` and on delete) lets this token
  take over an agent row owned by a different principal, including an
  organically header-asserted agent that has no owner yet (registering an
  identity that collides with one of those otherwise `409`s). It only works if
  the token itself was minted with adopt permission (`allowAdopt: true` at
  mint time, scope `agents:automation:adopt`) — otherwise the API returns
  `403`, which surfaces as a resource error regardless of how `adopt` is
  set in configuration.
- **Re-applying after `terraform destroy`** reactivates the archived agent
  back into its boundary rather than minting a new one — the underlying
  agent row isn't hard-deleted, `terraform destroy` archives it.

## Ordering: compute-early, register-late

Putting the two together is the point of this provider. See
[`examples/agentcore`](examples/agentcore) for a full AWS Bedrock AgentCore
example and the reasoning behind the ordering, and
[`examples/ecs`](examples/ecs) for the same ordering applied to a plain ECS
service:

```
1. data.alterion_agent_path_key       →  gateway_base_url (deterministic, no agent needed yet)
2. <cloud workload resource>          →  created with gateway_base_url baked into its env vars
3. alterion_agent                     →  registered now that the workload's resource id exists, depends_on the workload
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
