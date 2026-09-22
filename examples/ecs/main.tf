terraform {
  required_providers {
    alterion = {
      source = "alterion/alterion"
    }
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

# Minimal example: registers an agent running as an AWS ECS service. This
# is deliberately similar to examples/agentcore, except the workload isn't
# an AWS Bedrock AgentCore runtime — it proves the alterion_agent /
# alterion_agent_path_key surface is cloud- and workload-agnostic, not
# AgentCore-specific.

variable "aws_region" {
  type        = string
  description = "AWS region the ECS service runs in."
}

variable "service_name" {
  type        = string
  description = "Name of the ECS service (workload_name). Case-sensitive."
}

variable "environment" {
  type        = string
  description = "One of production, staging, development."

  validation {
    condition     = contains(["production", "staging", "development"], var.environment)
    error_message = "environment must be one of: production, staging, development."
  }
}

variable "orion_boundary" {
  type        = string
  description = "Name of the Orion contextual boundary this agent is approved into on registration. Required here (no default): when omitted, the agent lands in Shadow and is only captured, not enforced, which is not the intended default for this example."
}

variable "cluster_arn" {
  type        = string
  description = "ARN of the existing ECS cluster the service runs on."
}

variable "task_definition_arn" {
  type        = string
  description = "ARN of the task definition the service runs."
}

provider "aws" {
  region = var.aws_region
}

data "aws_caller_identity" "current" {}

provider "alterion" {
  # orion_url and api_token can also come from ALTERION_ORION_URL /
  # ALTERION_API_TOKEN, which is the recommended way to avoid committing
  # a token to version control.
  orion_url = "https://orion.example.com"
}

resource "aws_ecs_service" "this" {
  name            = var.service_name
  cluster         = var.cluster_arn
  task_definition = var.task_definition_arn
  desired_count   = 1
}

# Register the agent now that the service exists and has an id (which, for
# aws_ecs_service, is itself the ARN). Same shape as examples/agentcore's
# resource "alterion_agent" block, just with cloud_provider defaulting to
# "aws" and a different workload_type — no AgentCore-specific attributes
# anywhere on this resource.
resource "alterion_agent" "this" {
  environment            = var.environment
  cloud_account_id       = data.aws_caller_identity.current.account_id
  cloud_region           = var.aws_region
  workload_name          = var.service_name
  workload_resource_id   = aws_ecs_service.this.id
  workload_type          = "ecs-service"
  auto_register_boundary = var.orion_boundary
}

output "agent_id" {
  value = alterion_agent.this.agent_id
}
