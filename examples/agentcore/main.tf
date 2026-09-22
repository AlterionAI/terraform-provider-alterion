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

variable "aws_region" {
  type        = string
  description = "AWS region the AgentCore runtime is deployed in."
}

variable "workload_name" {
  type        = string
  description = "Name of the AgentCore runtime (agent_runtime_name). Case-sensitive; must match AWS Bedrock AgentCore's own naming rule: starts with a letter, then letters/digits/underscores, up to 48 characters. Used for both the data source and the resource, so they can't drift."
}

variable "agent_role_arn" {
  type        = string
  description = "IAM role the AgentCore runtime assumes."
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

  # Optional: lets the data source compute gateway_base_url locally if the
  # server ever returns a null one.
  gateway_url = "https://gw.example.com"

  # Set once here so neither the data source nor the resource below repeats
  # them; cloud_provider defaults to "aws" on the provider itself too.
  cloud_account_id = data.aws_caller_identity.current.account_id
  cloud_region     = var.aws_region
}

# STEP 1 — compute the agent's gateway path key BEFORE the runtime exists.
# This is deterministic (a hash of environment + the workload's identity),
# so it can be known ahead of time and baked into the runtime's own
# environment variables at creation, instead of requiring a second deploy
# once the agent is registered. Uses var.workload_name directly, the same
# variable the resource below uses — never the runtime resource's own name
# attribute, so the two can't drift.
data "alterion_agent_path_key" "this" {
  environment   = var.environment
  workload_name = var.workload_name
}

# STEP 2 — create the runtime, pointing its outbound LLM traffic at the
# gateway path key computed above.
resource "aws_bedrockagentcore_agent_runtime" "this" {
  agent_runtime_name = var.workload_name
  role_arn           = var.agent_role_arn

  agent_runtime_artifact {
    container_configuration {
      container_uri = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${var.aws_region}.amazonaws.com/example-agent:latest"
    }
  }

  network_configuration {
    network_mode = "PUBLIC"
  }

  environment_variables = {
    OPENAI_BASE_URL    = data.alterion_agent_path_key.this.gateway_base_url
    ANTHROPIC_BASE_URL = data.alterion_agent_path_key.this.gateway_base_url
  }
}

# STEP 3 — register the agent with Orion now that the runtime exists and
# has an ARN. workload_name is var.workload_name again (not the runtime
# resource's own name attribute); workload_resource_id references the
# runtime so Terraform infers the dependency, and depends_on is kept
# explicit anyway for clarity.
resource "alterion_agent" "this" {
  environment           = var.environment
  workload_name         = var.workload_name
  workload_resource_id  = aws_bedrockagentcore_agent_runtime.this.agent_runtime_arn
  workload_type         = "bedrock-agentcore-runtime"
  functional_boundaries = var.functional_boundaries

  depends_on = [aws_bedrockagentcore_agent_runtime.this]
}

output "gateway_base_url" {
  value = data.alterion_agent_path_key.this.gateway_base_url
}

output "agent_id" {
  value = alterion_agent.this.agent_id
}
