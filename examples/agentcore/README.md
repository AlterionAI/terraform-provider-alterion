# AgentCore example: compute-early, register-late

Wires an AWS Bedrock AgentCore runtime up to Alterion Orion's gateway, in the
order that avoids a chicken-and-egg deploy problem. See
[`examples/ecs`](../ecs) for the same pattern applied to a plain AWS ECS
service — `alterion_agent` / `alterion_agent_path_key` have nothing
AgentCore-specific about them.

## Why this ordering

The runtime needs its own gateway URL (`OPENAI_BASE_URL` /
`ANTHROPIC_BASE_URL`) baked into its environment variables **at creation
time**, but you don't want to register the agent with Orion before the
runtime exists either. Orion's asserted-agent path key resolves this
because it's **deterministic** — a hash of `environment` + the workload's
identity — so it can be computed before the runtime, or the agent
registration, exist:

1. **Compute early** — `data "alterion_agent_path_key"` resolves the gateway
   URL from `environment` + `workload_name` alone (cloud identity comes from
   the provider block).
2. **Create the runtime** — pass that URL into the runtime's environment
   variables at creation time.
3. **Register late** — `resource "alterion_agent"` registers the real agent
   row, now that the runtime exists and has an ARN to attach as
   `workload_resource_id`. `depends_on` keeps this explicit even though the
   ARN reference already implies it.

If step 3 fails or is deferred, the runtime still works — the gateway
resolves traffic by short id regardless of whether the agent row is fully
registered.

## Identity: provider-level cloud defaults, workload_name required

The agent's identity is `cloud_provider` + `cloud_account_id` +
`cloud_region` + `workload_name`. This example sets `cloud_account_id` and
`cloud_region` **once**, on the `provider "alterion"` block, from
`data.aws_caller_identity.current.account_id` and `var.aws_region` — the
provider has no way to read AWS credentials itself, so this account id has
to come from the `aws` provider. Both the data source and the resource then
omit them and inherit the provider's values; `cloud_provider` defaults to
`aws` on the provider too.

`workload_name` stays **required** on both the data source and the
resource — the identity has to exist before the runtime does, so there's
nothing to default it from. This example uses the single `var.workload_name`
for both, never the AgentCore runtime resource's own `agent_runtime_name`
attribute, so the two can't drift apart.

## Where the gateway URL actually goes

`data.alterion_agent_path_key.this.gateway_base_url` is only a computed
value until something reads it. Here that's the runtime's own
`environment_variables` block, above: both `OPENAI_BASE_URL` and
`ANTHROPIC_BASE_URL` are set to it, since this provider has no way to know
which SDK the agent's code actually uses. This provider itself never
touches `aws_bedrockagentcore_agent_runtime` or any other cloud resource —
that wiring lives entirely in this Terraform config, using the AWS
provider's own resource types. See the root
[README](../../README.md#how-the-agent-gets-its-gateway-url) for the full
variable-name table across SDKs.

## Boundaries

Registering an agent always joins it to the environment boundary
(Production/Staging/Development) derived from `var.environment` — no
configuration needed for that. `functional_boundaries` is the optional,
additional list of functional Orion boundaries this agent should also join;
it defaults to `[]`. There's no longer an `auto_register_boundary`
attribute — an agent that registers with no `functional_boundaries` still
lands in its environment boundary, not in Shadow.

## Ownership and `adopt`

If the resulting identity already exists as an organically header-asserted
agent (one Orion picked up on its own, with no owner yet), registration
`409`s unless you set `adopt = true` on `alterion_agent.this` — and that
only works if `ALTERION_API_TOKEN` was minted with adopt permission. See the
root [README](../../README.md#ownership-and-adopt) for the full contract.

## Running this example

```bash
export ALTERION_API_TOKEN=orion_at_...
terraform init
terraform plan \
  -var="aws_region=us-east-1" \
  -var="workload_name=support-bot" \
  -var="environment=production" \
  -var="orion_gateway_url=https://gw.example.com" \
  -var='functional_boundaries=["production-support"]'
```

This example is for illustration — it references `aws_bedrockagentcore_agent_runtime`
as documented by the AWS provider at the time of writing; check the AWS
provider's current schema for that resource before using this in a real
deployment (attribute names for Bedrock AgentCore are still evolving
upstream).
