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

variable "workload_name" {
  type        = string
  description = "Name of the ECS service. Case-sensitive. Used for both the data source and the resource, so they can't drift."
}

variable "environment" {
  type        = string
  description = "One of production, staging, development."

  validation {
    condition     = contains(["production", "staging", "development"], var.environment)
    error_message = "environment must be one of: production, staging, development."
  }
}

variable "functional_boundaries" {
  type        = list(string)
  default     = []
  description = "Functional Orion boundaries the agent also joins, beyond the environment boundary derived from var.environment."
}

variable "cluster_arn" {
  type        = string
  description = "ARN of the existing ECS cluster the service runs on."
}

variable "container_name" {
  type        = string
  description = "Name of the container in the task definition that runs the agent."
}

variable "container_image" {
  type        = string
  description = "Container image for the agent (e.g. an ECR image URI)."
}

variable "task_execution_role_arn" {
  type        = string
  description = "IAM role ECS uses to pull the image and write logs."
}

variable "orion_gateway_url" {
  type        = string
  description = "Public base URL of the Orion AI gateway (or set ALTERION_GATEWAY_URL instead)."
}

provider "aws" {
  region = var.aws_region
}

# This provider has no access to AWS credentials, so it can't discover the
# account id itself — resolve it once here and feed it to the alterion
# provider block below.
data "aws_caller_identity" "current" {}

provider "alterion" {
  # orion_url and api_token can also come from ALTERION_ORION_URL /
  # ALTERION_API_TOKEN, which is the recommended way to avoid committing
  # a token to version control.
  orion_url = "https://orion.example.com"

  # Required: the data source's gateway_base_url is built from this and
  # must be injected into the task definition's container environment
  # below.
  gateway_url = var.orion_gateway_url

  # Set once here so neither the data source nor the resource below repeats
  # them; cloud_provider defaults to "aws" on the provider itself too.
  cloud_account_id = data.aws_caller_identity.current.account_id
  cloud_region     = var.aws_region
}

# STEP 1 — compute the agent's gateway path key BEFORE the service exists.
# Uses var.workload_name directly, the same variable the resource below
# uses — never the AWS resource's own name attribute, so the two can't
# drift.
data "alterion_agent_path_key" "this" {
  environment   = var.environment
  workload_name = var.workload_name
}

# Task definition carries the gateway URL into the container's own
# environment. Orion gateway URL from the data source; pick the variable
# your SDK reads.
resource "aws_ecs_task_definition" "this" {
  family                   = var.workload_name
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = var.task_execution_role_arn

  container_definitions = jsonencode([
    {
      name  = var.container_name
      image = var.container_image
      environment = [
        { name = "OPENAI_BASE_URL", value = data.alterion_agent_path_key.this.gateway_base_url },
        { name = "ANTHROPIC_BASE_URL", value = data.alterion_agent_path_key.this.gateway_base_url },
      ]
    }
  ])
}

resource "aws_ecs_service" "this" {
  name            = var.workload_name
  cluster         = var.cluster_arn
  task_definition = aws_ecs_task_definition.this.arn
  desired_count   = 1
}

# STEP 2 — register the agent now that the service exists and has an id
# (which, for aws_ecs_service, is itself the ARN).
resource "alterion_agent" "this" {
  environment           = var.environment
  workload_name         = var.workload_name
  workload_resource_id  = aws_ecs_service.this.id
  workload_type         = "ecs-service"
  functional_boundaries = var.functional_boundaries

  depends_on = [aws_ecs_service.this]
}

output "agent_id" {
  value = alterion_agent.this.agent_id
}
