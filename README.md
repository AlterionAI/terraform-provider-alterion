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

  # Required (or ALTERION_GATEWAY_URL): the Orion AI gateway's public base
  # URL, no path. The alterion_agent_path_key data source's gateway_base_url
  # is always built from it — either the server's own gatewayBaseUrl, or
  # "<gateway_url>/<path_prefix>/<short_id>" when the server returns null.
  gateway_url = "https://gw.example.com"

  # Optional defaults for cloud_provider/cloud_account_id/cloud_region on
  # every alterion_agent and alterion_agent_path_key below that omits them.
  # This provider has no way to read your cloud credentials itself, so
  # cloud_account_id/cloud_region still have to come from somewhere — set
  # them here once (e.g. from data.aws_caller_identity.current.account_id)
  # instead of repeating them on every resource/data source.
  cloud_provider   = "aws"           # one of aws, gcp, azure; defaults to aws
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
}
```

## The two building blocks

Both resources use the same **cloud-agnostic identity**: `environment` +
`cloud_provider` + `cloud_account_id` + `cloud_region` + `workload_name`.
`cloud_provider`/`cloud_account_id`/`cloud_region` are optional on both and
fall back to the provider block's defaults above; it's an error if neither
level sets `cloud_account_id` or `cloud_region`. `workload_name` is
**required on both** — the identity has to exist before the runtime does,
so there's nothing to default it from. None of this is tied to any one
cloud service — see [`examples/agentcore`](examples/agentcore) (AWS Bedrock
AgentCore) and [`examples/ecs`](examples/ecs) (a plain AWS ECS service) for
two different workload types registered through the identical resource
shape, both feeding the same `workload_name` variable to the data source
and the resource so the two can't drift apart.

### `data "alterion_agent_path_key"`

Resolves an agent's deterministic gateway path key from the identity alone
— no agent needs to exist yet. Use this to compute a runtime's outbound
gateway URL *before* creating the runtime.

```hcl
data "alterion_agent_path_key" "this" {
  environment   = "production"
  workload_name = "support-bot"
}

output "gateway_base_url" {
  value = data.alterion_agent_path_key.this.gateway_base_url
}
```

## How the agent gets its gateway URL

This is the point of the whole flow, so it's worth stating plainly:

1. The `alterion_agent_path_key` data source returns `gateway_base_url` —
   always a complete URL, never null.
2. Your cloud workload resource (an `aws_bedrockagentcore_agent_runtime`,
   an ECS task definition, a Cloud Run service, etc.) sets its LLM SDK's
   base-URL environment variable(s) to that value, in the **same
   `terraform apply`** that creates the workload.
3. **This provider never modifies any cloud resource itself.** It only
   resolves and registers Orion-side state; wiring the value into the
   runtime's environment is your Terraform config's job, using the cloud
   provider's own resource types.

The variable **name** depends on which SDK the agent uses — this provider
has no way to know that, so it doesn't guess:

| Agent's LLM SDK   | Environment variable  |
| ----------------- | ---------------------- |
| OpenAI SDK        | `OPENAI_BASE_URL`      |
| Anthropic SDK      | `ANTHROPIC_BASE_URL`   |
| LangChain / others | its own base-url setting (e.g. a client constructor argument, not always an env var) |

The URL has to be **baked into the runtime at create time** — most
runtimes don't hot-reload environment variables — which is why the data
source resolves at plan time (step 1) and `alterion_agent` registration
happens after the workload exists (step 3 below): the gateway URL is
deterministic and doesn't depend on the agent being registered yet, so
nothing blocks on that ordering.

### `resource "alterion_agent"`

Registers (creates/updates/deletes) the asserted agent row itself. The
identity attributes (`environment`, `cloud_provider`, `cloud_account_id`,
`cloud_region`, `workload_name`) force replacement if changed; every other
attribute updates in place via a re-`POST` (the server upserts on
`environment` + the identity).

```hcl
resource "alterion_agent" "this" {
  environment           = "production"
  workload_name         = aws_bedrockagentcore_agent_runtime.this.agent_runtime_name
  workload_resource_id  = aws_bedrockagentcore_agent_runtime.this.agent_runtime_arn
  workload_type         = "bedrock-agentcore-runtime"
  functional_boundaries = ["production-support"]
}
```

`workload_resource_id` (the workload's own post-create unique id: an AWS
ARN, a GCP full resource name, or an Azure resource id) and `workload_type`
are both optional; when `workload_type` is omitted the server infers it
from `workload_resource_id`. `display_name` is optional and defaults to
`workload_name` when omitted.

Registration always joins the agent to the **environment boundary**
(Production/Staging/Development), derived server-side from `environment` —
no configuration needed for that. `functional_boundaries` is an optional
list of additional functional Orion boundary names the agent also joins;
it defaults to an empty list.

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
1. data.alterion_agent_path_key       →  gateway_base_url, resolved at plan time from environment + workload_name
2. <cloud workload resource>          →  created with gateway_base_url baked into its env vars
3. alterion_agent                     →  registered now that the workload's resource id exists, depends_on the workload
```

## Local development

Requires Go 1.27+ and Terraform (or OpenTofu).

```bash
go build -o terraform-provider-alterion .
go vet ./...
gofmt -l .          # must print nothing
go test ./...       # unit tests (internal/client + provider validators) — no network access
TF_ACC=1 go test ./... -run TestAcc -v   # + acceptance-style tests (internal/provider), against an in-process fake Orion server, not a real deployment
```

A `Makefile` wraps the common targets: `make build`, `make test`, `make testacc`, `make lint`, `make docs`, `make snapshot`.

## Testing

Four layers, from fastest/cheapest to slowest/most expensive:

1. **Unit — `internal/client`.** Table-driven `httptest` coverage of every
   route × status the API contract lists (`internal/client/client_test.go`),
   plus golden short-id vectors and identity-key composition across all
   three clouds (`internal/client/contract_test.go`). No network, no
   `terraform` binary. `make test` or `go test ./internal/client/...`.
2. **Unit — schema validators (`internal/provider/validators_test.go`).**
   Drives each `validator.String` (cloud_account_id/cloud_region/
   workload_name/environment/cloud_provider/workload_resource_id) directly
   against a real two-attribute framework schema, table-driven across
   aws/gcp/azure shapes — including GCP project-id min/max length and
   trailing-hyphen, and Azure GUID uppercase. No `TF_ACC`/`terraform`
   binary needed.
3. **Acceptance — `internal/provider/{provider,acceptance_matrix}_test.go`.**
   `terraform-plugin-testing` `resource.Test` against an in-process fake
   Orion server (`httptest`), never a real deployment. Covers plan/apply
   precedence (provider defaults vs. resource override), `RequiresReplace`
   on every identity attribute, in-place updates, the lifecycle run once
   per cloud (`TestAccAgentResource_LifecycleMatrix`), and every error path
   (401/403/404/409/400-boundary, adopt, flag-off 404). Requires
   `TF_ACC=1` and a `terraform` binary on `PATH` (or `TF_ACC_TERRAFORM_PATH`
   pointed at an OpenTofu binary, with `TF_ACC_PROVIDER_NAMESPACE=hashicorp`
   — verified locally against OpenTofu 1.12). `make testacc`.
4. **Live (opt-in) — `internal/acceptance_live/live_test.go`.** Runs
   create → read → update → destroy against a **real** Orion deployment.
   Skipped unless `TF_ACC_LIVE=1` plus `ALTERION_ORION_URL`,
   `ALTERION_API_TOKEN`, `ALTERION_GATEWAY_URL`, `ALTERION_TEST_ACCOUNT_ID`,
   `ALTERION_TEST_REGION` are all set; uses a timestamp-suffixed
   `workload_name` per run and always archives the agent on cleanup (via
   `terraform-plugin-testing`'s own post-test destroy). `make testacc-live`.
   Runs nightly in CI (`.github/workflows/nightly-live.yml`,
   `workflow_dispatch` too) against the `ALTERION_ORION_URL`/
   `ALTERION_API_TOKEN`/`ALTERION_GATEWAY_URL`/`ALTERION_TEST_ACCOUNT_ID`/
   `ALTERION_TEST_REGION` repo secrets; the job skips cleanly (a
   `::warning::`, not a failure) when those secrets aren't configured.

**Schema snapshot** (`internal/provider/schema_snapshot_test.go`) marshals
the provider/resource/data-source schemas via `GetProviderSchema` and
diffs them against `testdata/schema.json` — the customer-facing contract.
Regenerate after an intentional schema change: `make snapshot` (or
`UPDATE_SNAPSHOT=1 go test ./internal/provider/... -run TestSchemaSnapshot`).

**CI** (`.github/workflows/ci.yml`): `gitleaks`, `lint` (gofmt + vet +
golangci-lint), `unit` (layers 1-2 above, with an 85% coverage gate on
`internal/client`), `acceptance` (layer 3, matrixed over Terraform 1.5.7,
1.9.x, latest, and OpenTofu latest), `schema-snapshot`, `docs-drift`
(`tfplugindocs validate` + `generate`, fails on any diff under `docs/`),
and `examples` (builds the provider, writes a `dev_overrides` CLI config,
then `terraform init`/`validate` against each example with real registry
network access). `.github/workflows/nightly-live.yml` runs layer 4 nightly
plus on-demand via `workflow_dispatch`.

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

## Changes in this version

- Provider-level `cloud_provider` / `cloud_account_id` / `cloud_region`
  defaults; the same three attributes are now optional (not required) on
  both the resource and the data source.
- `auto_register_boundary` removed; replaced by `functional_boundaries`
  (a list) on `alterion_agent`. The environment boundary is always derived
  from `environment` — no separate opt-in needed for that.
- `gateway_url` is now **required** (set directly or via
  `ALTERION_GATEWAY_URL`) and validated as an absolute `https://`/`http://`
  URL with no path. `gateway_base_url` on `alterion_agent_path_key` is now
  guaranteed to always be a complete URL, never null.

## License

[MPL-2.0](LICENSE), consistent with HashiCorp's own providers.
