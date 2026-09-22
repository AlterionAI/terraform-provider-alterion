# ECS example: cloud-agnostic surface, non-AgentCore workload

This example registers an agent running as a plain AWS ECS service. It's
deliberately almost identical to [`examples/agentcore`](../agentcore),
except the workload is `aws_ecs_service` instead of
`aws_bedrockagentcore_agent_runtime` — the point is to prove that
`alterion_agent` / `alterion_agent_path_key` have nothing AgentCore-specific
about them. The same four identity attributes (`cloud_provider`,
`cloud_account_id`, `cloud_region`, `workload_name`) apply to any workload
on any of the three supported clouds; `workload_resource_id` and
`workload_type` are free-form metadata, not tied to any one AWS service.

Here, `workload_resource_id` is `aws_ecs_service.this.id`, which for
`aws_ecs_service` is itself the service's ARN, and `workload_type` is set to
`"ecs-service"` instead of `"bedrock-agentcore-runtime"`.

## Running this example

```bash
export ALTERION_API_TOKEN=orion_at_...
terraform init
terraform plan \
  -var="aws_region=us-east-1" \
  -var="service_name=support-bot" \
  -var="environment=production" \
  -var="orion_boundary=production-support" \
  -var="cluster_arn=arn:aws:ecs:us-east-1:123456789012:cluster/example" \
  -var="task_definition_arn=arn:aws:ecs:us-east-1:123456789012:task-definition/example:1"
```

See the root [README](../../README.md) for the full attribute reference and
ownership/`adopt` contract.
