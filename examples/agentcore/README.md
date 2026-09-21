# AgentCore example: compute-early, register-late

This example wires an AWS Bedrock AgentCore runtime up to Alterion Orion's
gateway, in the order that avoids a chicken-and-egg deploy problem.

## Why this ordering

An agent runtime needs to know its own gateway URL (to send LLM traffic
through `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL`) **at creation time** — it's
baked into the container's environment variables. But the natural instinct
is to register the agent with Orion *after* the runtime exists, once you
have a real `runtime_arn` to hand it. Do it in that order and you hit a
catch-22: the runtime needs the gateway URL before it exists, but you don't
want to register the agent (and mint a real, permanent identity for it)
before the runtime exists either.

Orion's asserted-agent path key resolves this because it's **deterministic**:
the short id embedded in the gateway URL is just
`sha256("asserted|<environment>|<slug>")`, truncated to 12 hex characters. It
depends only on the environment and slug you choose — never on whether the
agent has actually been registered yet. So the gateway URL can be computed
before the runtime exists, with no risk of it changing out from under you
once registration happens for real.

That gives two steps that can run in the right order:

1. **Compute early** — `data "alterion_agent_path_key"` resolves the gateway
   URL from `environment` + `slug` alone. No agent has to exist yet.
2. **Create the runtime** — pass that URL into the runtime's environment
   variables at creation time.
3. **Register late** — `resource "alterion_agent"` registers the real agent
   row, now that the runtime exists and has a `runtime_arn` to attach.

If step 3 fails or is deferred, the runtime still works — the gateway
resolves traffic by short id regardless of whether the asserted-agent row is
fully registered. Registration is what makes the agent visible and governed
in Orion, but it doesn't gate the runtime's ability to talk to the gateway.

## Slug convention

The recommended slug shape is:

```
<aws-account-id>-<region>-<runtime-name, lowercased, non-alphanumerics collapsed to hyphens>
```

e.g. `123456789012-us-east-1-support-bot`. This keeps slugs unique across
accounts/regions without a central registry, and keeps them within the
Orion API's 64-character limit for realistic runtime names — see the
`runtime_name` variable's `validation` block in `main.tf` for the length
check.

## Running this example

```bash
export ALTERION_API_TOKEN=orion_at_...
terraform init
terraform plan \
  -var="aws_account_id=123456789012" \
  -var="aws_region=us-east-1" \
  -var="runtime_name=support-bot" \
  -var="environment=production"
```

This example is for illustration — it references `aws_bedrockagentcore_agent_runtime`
as documented by the AWS provider at the time of writing; check the AWS
provider's current schema for that resource before using this in a real
deployment (attribute names for Bedrock AgentCore are still evolving
upstream).
