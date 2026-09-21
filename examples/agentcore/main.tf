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

variable "aws_account_id" {
  type        = string
  description = "AWS account id the AgentCore runtime is deployed in."
}

variable "aws_region" {
  type        = string
  description = "AWS region the AgentCore runtime is deployed in."
}

variable "runtime_name" {
  type        = string
  description = "Human-readable name of the AgentCore runtime."

  validation {
    # aws_account_id + "-" + aws_region + "-" + slugified runtime_name must
    # fit in the 64 character slug limit enforced by the Orion API. This
    # check only bounds the runtime_name part; combine with your own
    # account id / region lengths when reviewing real values.
    condition     = length(var.runtime_name) <= 64
    error_message = "runtime_name must be at most 64 characters so the derived slug (\"${var.aws_account_id}-${var.aws_region}-<slug of runtime_name>\") stays within the Orion API's 64 character slug limit."
  }
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

locals {
  # Recommended slug convention: <aws-account-id>-<region>-<runtime-name>,
  # lowercased and with anything other than a-z0-9 collapsed to a single
  # hyphen, matching the Orion API's slug regex.
  slug = "${var.aws_account_id}-${var.aws_region}-${lower(replace(var.runtime_name, "/[^A-Za-z0-9]+/", "-"))}"
}

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
# This is deterministic (a hash of environment + slug), so it can be known
# ahead of time and baked into the runtime's own environment variables at
# creation, instead of requiring a second deploy once the agent is
# registered.
data "alterion_agent_path_key" "this" {
  environment = var.environment
  slug        = local.slug
}

# STEP 2 — create the runtime, pointing its outbound LLM traffic at the
# gateway path key computed above. Resource name per the AWS provider's
# naming for Bedrock AgentCore; verify against the installed aws provider
# version if this differs.
resource "aws_bedrockagentcore_agent_runtime" "this" {
  agent_runtime_name = var.runtime_name

  agent_runtime_artifact {
    container_configuration {
      container_uri = "123456789012.dkr.ecr.us-east-1.amazonaws.com/example-agent:latest"
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
# has an ARN. depends_on is explicit (not inferred) because runtime_arn is
# a plain string, not a reference the provider graph would otherwise pick
# up as a dependency edge on its own here.
resource "alterion_agent" "this" {
  environment            = var.environment
  slug                   = local.slug
  display_name           = var.runtime_name
  runtime_arn            = aws_bedrockagentcore_agent_runtime.this.agent_runtime_arn
  auto_register_boundary = var.orion_boundary

  aws_account_id = var.aws_account_id
  region         = var.aws_region

  depends_on = [aws_bedrockagentcore_agent_runtime.this]
}

output "gateway_base_url" {
  value = data.alterion_agent_path_key.this.gateway_base_url
}

output "agent_id" {
  value = alterion_agent.this.agent_id
}
