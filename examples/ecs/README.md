# ECS example: cloud-agnostic surface, non-AgentCore workload

Registers an agent running as a plain AWS ECS service. Deliberately almost
identical to [`examples/agentcore`](../agentcore), except the workload is
`aws_ecs_service` instead of `aws_bedrockagentcore_agent_runtime` — the
point is to prove `alterion_agent` / `alterion_agent_path_key` have nothing
AgentCore-specific about them.

## Identity: provider-level cloud defaults, workload_name required

`cloud_account_id` and `cloud_region` are set **once**, on the
`provider "alterion"` block, from `data.aws_caller_identity.current.account_id`
and `var.aws_region` — this provider can't read AWS credentials itself, so
the account id has to come from the `aws` provider. The data source and the
resource below both omit them and inherit the provider's values;
`cloud_provider` defaults to `aws` on the provider too.

`workload_name` stays **required** on both the data source and the
resource — the identity has to exist before the runtime does. This example
uses the single `var.workload_name` for both, never `aws_ecs_service`'s own
`name` attribute, so the two can't drift apart.

## Ordering

Same as [`examples/agentcore`](../agentcore):

```
1. data.alterion_agent_path_key   →  resolved at plan time from environment + workload_name
2. aws_ecs_service                →  created
3. alterion_agent                 →  registered, depends_on the service
```

`workload_resource_id` here is `aws_ecs_service.this.id`, which for
`aws_ecs_service` is itself the service's ARN; `workload_type` is
`"ecs-service"`.

## Boundaries

Registering an agent always joins it to the environment boundary
(Production/Staging/Development) derived from `var.environment`.
`functional_boundaries` is the optional, additional list of functional
Orion boundaries this agent should also join; it defaults to `[]`. There's
no `auto_register_boundary` attribute anymore.

## Running this example

```bash
export ALTERION_API_TOKEN=orion_at_...
terraform init
terraform plan \
  -var="aws_region=us-east-1" \
  -var="workload_name=support-bot" \
  -var="environment=production" \
  -var='functional_boundaries=["production-support"]' \
  -var="cluster_arn=arn:aws:ecs:us-east-1:123456789012:cluster/example" \
  -var="task_definition_arn=arn:aws:ecs:us-east-1:123456789012:task-definition/example:1"
```

See the root [README](../../README.md) for the full attribute reference and
ownership/`adopt` contract.
