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

variable "runtime_name" {
  type        = string
  description = "Name of the AgentCore runtime (agent_runtime_name). Case-sensitive; must match AWS Bedrock AgentCore's own naming rule: starts with a letter, then letters/digits/underscores, up to 48 characters."
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

provider "aws" {
  region = var.aws_region
}

# Used to fill cloud_account_id below without hardcoding it — this example
# never asks the caller for their AWS account id directly.
data "aws_caller_identity" "current" {}

provider "alterion" {
  # orion_url and api_token can also come from ALTERION_ORION_URL /
  # ALTERION_API_TOKEN, which is the recommended way to avoid committing
  # a token to version control.
  orion_url = "https://orion.example.com"

  # Optional: lets the data source compute gateway_base_url locally if the
  # server ever returns a null one.
  gateway_url = "https://gw.example.com"
}

# STEP 1 — compute the agent's gateway path key BEFORE the runtime exists.
# This is deterministic (a hash of environment + the workload's identity),
# so it can be known ahead of time and baked into the runtime's own
# environment variables at creation, instead of requiring a second deploy
# once the agent is registered. Note this data source uses var.runtime_name
# directly, not the resource's attribute below — it cannot depend on a
# runtime that doesn't exist yet. cloud_provider defaults to "aws" and is
# left unset here.
data "alterion_agent_path_key" "this" {
  environment      = var.environment
  cloud_account_id = data.aws_caller_identity.current.account_id
  cloud_region     = var.aws_region
  workload_name    = var.runtime_name
}

# STEP 2 — create the runtime, pointing its outbound LLM traffic at the
# gateway path key computed above. Resource name per the AWS provider's
# naming for Bedrock AgentCore; verify against the installed aws provider
# version if this differs.
resource "aws_bedrockagentcore_agent_runtime" "this" {
  agent_runtime_name = var.runtime_name

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
# has an ARN. workload_name and workload_resource_id both reference the
# runtime resource directly, so Terraform infers the dependency; depends_on
# is kept explicit anyway for clarity.
resource "alterion_agent" "this" {
  environment            = var.environment
  cloud_account_id       = data.aws_caller_identity.current.account_id
  cloud_region           = var.aws_region
  workload_name          = aws_bedrockagentcore_agent_runtime.this.agent_runtime_name
  workload_resource_id   = aws_bedrockagentcore_agent_runtime.this.agent_runtime_arn
  workload_type          = "bedrock-agentcore-runtime"
  auto_register_boundary = var.orion_boundary

  depends_on = [aws_bedrockagentcore_agent_runtime.this]
}

output "gateway_base_url" {
  value = data.alterion_agent_path_key.this.gateway_base_url
}

output "agent_id" {
  value = alterion_agent.this.agent_id
}
