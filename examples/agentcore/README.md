# AgentCore example: compute-early, register-late

This example wires an AWS Bedrock AgentCore runtime up to Alterion Orion's
gateway, in the order that avoids a chicken-and-egg deploy problem. See
[`examples/ecs`](../ecs) for the same pattern applied to a plain AWS ECS
service — the `alterion_agent` / `alterion_agent_path_key` surface itself
has nothing AgentCore-specific about it.

## Why this ordering

An agent runtime needs to know its own gateway URL (to send LLM traffic
through `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL`) **at creation time** — it's
baked into the container's environment variables. But the natural instinct
is to register the agent with Orion *after* the runtime exists, once you
have a real ARN to hand it. Do it in that order and you hit a catch-22: the
runtime needs the gateway URL before it exists, but you don't want to
register the agent (and mint a real, permanent identity for it) before the
runtime exists either.

Orion's asserted-agent path key resolves this because it's **deterministic**:
the short id embedded in the gateway URL is just a hash of the environment
plus the workload's identity — cloud provider, cloud account id, cloud
region, and workload name. It depends only on those values, never on
whether the agent has actually been registered yet. So the gateway URL can
be computed before the runtime exists, with no risk of it changing out from
under you once registration happens for real.

That gives two steps that can run in the right order:

1. **Compute early** — `data "alterion_agent_path_key"` resolves the gateway
   URL from `environment` + `cloud_account_id` + `cloud_region` +
   `workload_name` alone (`cloud_provider` defaults to `aws`). No agent has
   to exist yet.
2. **Create the runtime** — pass that URL into the runtime's environment
   variables at creation time.
3. **Register late** — `resource "alterion_agent"` registers the real agent
   row, now that the runtime exists and has an ARN to attach as
   `workload_resource_id`.

If step 3 fails or is deferred, the runtime still works — the gateway
resolves traffic by short id regardless of whether the asserted-agent row is
fully registered. Registration is what makes the agent visible and governed
in Orion, but it doesn't gate the runtime's ability to talk to the gateway.

This example sets `auto_register_boundary` from the required `orion_boundary`
variable so the agent is approved into a real boundary the moment it
registers; if you omit `auto_register_boundary` in your own configuration,
the agent instead lands in Shadow, where it's only captured, not enforced.

## Identity: cloud, account, region, and workload name

The agent's identity within an environment is the tuple `cloud_provider` +
`cloud_account_id` + `cloud_region` + `workload_name`. This keeps identities
unique across clouds/accounts/regions without a central registry, and
`workload_name` is used **exactly as given** — case-sensitive, no
lowercasing or hyphen-folding — so that two workloads whose names differ
only by case (e.g. `SupportBot` vs. `supportbot`) stay distinct Orion agents
too. `workload_resource_id` (this example: the AgentCore runtime's ARN) and
`workload_type` are optional metadata attached once the underlying resource
exists; they play no part in the identity itself.

If the resulting identity happens to already exist as an organically
header-asserted agent (one Orion picked up on its own, with no owner yet),
registration `409`s unless you set `adopt = true` on `alterion_agent.this`
— and that only works if `ALTERION_API_TOKEN` was minted with adopt
permission. See the root [README](../../README.md#ownership-and-adopt) for
the full ownership/adopt contract, including why rotating the token doesn't
require re-adopting anything and why `terraform destroy` + re-`apply`
reactivates the same agent instead of creating a new one.

## Running this example

```bash
export ALTERION_API_TOKEN=orion_at_...
terraform init
terraform plan \
  -var="aws_region=us-east-1" \
  -var="runtime_name=support-bot" \
  -var="environment=production" \
  -var="orion_boundary=production-support"
```

`cloud_account_id` is filled in automatically from `data.aws_caller_identity`
— you don't pass your AWS account id as a variable.

This example is for illustration — it references `aws_bedrockagentcore_agent_runtime`
as documented by the AWS provider at the time of writing; check the AWS
provider's current schema for that resource before using this in a real
deployment (attribute names for Bedrock AgentCore are still evolving
upstream).
