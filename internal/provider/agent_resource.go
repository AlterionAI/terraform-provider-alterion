package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlterionAI/alterion-terraform-provider/internal/client"
)

var (
	_ resource.Resource                = &agentResource{}
	_ resource.ResourceWithConfigure   = &agentResource{}
	_ resource.ResourceWithImportState = &agentResource{}
)

func NewAgentResource() resource.Resource {
	return &agentResource{}
}

type agentResource struct {
	client *client.Client
}

type agentResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Environment          types.String `tfsdk:"environment"`
	Slug                 types.String `tfsdk:"slug"`
	DisplayName          types.String `tfsdk:"display_name"`
	AutoRegisterBoundary types.String `tfsdk:"auto_register_boundary"`
	RuntimeARN           types.String `tfsdk:"runtime_arn"`
	AWSAccountID         types.String `tfsdk:"aws_account_id"`
	Region               types.String `tfsdk:"region"`
	Adopt                types.Bool   `tfsdk:"adopt"`
	AgentID              types.String `tfsdk:"agent_id"`
	ShortID              types.String `tfsdk:"short_id"`
	Status               types.String `tfsdk:"status"`
	IsRegistered         types.Bool   `tfsdk:"is_registered"`
}

func (r *agentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent"
}

func (r *agentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Registers an asserted Orion agent. This is the register-late half of the compute-early/register-late pattern: create the runtime first (using the alterion_agent_path_key data source to compute its gateway URL ahead of time), then register it here with the runtime's identifying details.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Same value as agent_id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"environment": schema.StringAttribute{
				Required:    true,
				Description: "One of production, staging, development. Changing this forces a new resource.",
				Validators: []validator.String{
					environmentValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Stable slug identifying the agent within the environment. Must match ^[a-z0-9]+(?:-[a-z0-9]+)*$ and be at most 64 characters. Changing this forces a new resource.",
				Validators: []validator.String{
					slugValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable display name for the agent.",
			},
			"auto_register_boundary": schema.StringAttribute{
				Optional:    true,
				Description: "Name of the Orion contextual boundary this agent is approved into on registration. When omitted, the agent lands in Shadow and is only captured, not enforced.",
			},
			"runtime_arn": schema.StringAttribute{
				Optional:    true,
				Description: "ARN of the underlying runtime (e.g. an AWS Bedrock AgentCore runtime), once it exists.",
			},
			"aws_account_id": schema.StringAttribute{
				Optional:    true,
				Description: "AWS account id the runtime is deployed in.",
			},
			"region": schema.StringAttribute{
				Optional:    true,
				Description: "Cloud region the runtime is deployed in.",
			},
			"adopt": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "When true, allows this resource to take over an agent row already owned by a different principal, and to delete it even if owned elsewhere. Defaults to false.",
			},
			"agent_id": schema.StringAttribute{
				Computed:    true,
				Description: "The full asserted agent id, of the form asserted|<environment>|<slug>.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"short_id": schema.StringAttribute{
				Computed:    true,
				Description: "12 hex character short id derived from the agent id.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Server-reported registration status.",
			},
			"is_registered": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether the agent is fully registered on the server.",
			},
		},
	}
}

func (r *agentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(*AlterionProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *provider.AlterionProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = data.Client
}

func (r *agentResource) createOrUpdate(ctx context.Context, model *agentResourceModel, diags *diag.Diagnostics) {
	req := client.CreateAgentRequest{
		Environment:          model.Environment.ValueString(),
		Slug:                 model.Slug.ValueString(),
		DisplayName:          model.DisplayName.ValueString(),
		AutoRegisterBoundary: model.AutoRegisterBoundary.ValueString(),
		RuntimeARN:           model.RuntimeARN.ValueString(),
		AWSAccountID:         model.AWSAccountID.ValueString(),
		Region:               model.Region.ValueString(),
		Adopt:                model.Adopt.ValueBool(),
	}

	result, err := r.client.CreateAgent(ctx, req)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 409 {
			diags.AddError(
				"Agent Owned By Another Principal",
				fmt.Sprintf("The Orion API reports environment=%q slug=%q is owned by a different principal: %s. Set adopt = true to take ownership.",
					req.Environment, req.Slug, apiErr.Message),
			)
			return
		}
		diags.AddError(
			"Error Creating/Updating Agent",
			fmt.Sprintf("Could not create/update agent environment=%q slug=%q: %s", req.Environment, req.Slug, err),
		)
		return
	}

	model.AgentID = types.StringValue(result.AgentID)
	model.ShortID = types.StringValue(result.ShortID)
	model.Status = types.StringValue(result.Status)
	model.IsRegistered = types.BoolValue(result.IsRegistered)
	model.ID = types.StringValue(result.AgentID)
}

func (r *agentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var model agentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var diags diag.Diagnostics
	r.createOrUpdate(ctx, &model, &diags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// Read refreshes state via GET /api/v1/agents/asserted/{shortId}.
// environment and slug are kept from state (the server doesn't echo them
// back on this route). short_id also normally comes from state; if state
// somehow has no short_id yet (e.g. an older state predating this field),
// it is derived once via the path-key lookup before the GET.
func (r *agentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	environment := model.Environment.ValueString()
	slug := model.Slug.ValueString()
	shortID := model.ShortID.ValueString()

	if shortID == "" {
		pathKey, err := r.client.GetPathKey(ctx, environment, slug)
		if err != nil {
			if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 404 {
				resp.State.RemoveResource(ctx)
				return
			}
			resp.Diagnostics.AddError(
				"Error Reading Agent",
				fmt.Sprintf("Could not resolve short id for agent environment=%q slug=%q: %s", environment, slug, err),
			)
			return
		}
		shortID = pathKey.ShortID
	}

	result, err := r.client.GetAgent(ctx, shortID)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 404 {
			// Agent missing or archived; remove from state.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Agent",
			fmt.Sprintf("Could not read agent short_id=%q: %s", shortID, err),
		)
		return
	}

	model.AgentID = types.StringValue(result.AgentID)
	model.ShortID = types.StringValue(result.ShortID)
	model.DisplayName = types.StringValue(result.DisplayName)
	model.Status = types.StringValue(result.Status)
	model.IsRegistered = types.BoolValue(result.IsRegistered)
	model.ID = types.StringValue(result.AgentID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *agentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var model agentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var diags diag.Diagnostics
	r.createOrUpdate(ctx, &model, &diags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *agentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var model agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	shortID := model.ShortID.ValueString()
	_, err := r.client.DeleteAgent(ctx, shortID, model.Adopt.ValueBool())
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 404 {
			// Already gone; treat as success.
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Agent",
			fmt.Sprintf("Could not delete agent short_id=%q: %s", shortID, err),
		)
		return
	}
}

// ImportState is out of scope: import-by-short-id would need a GET-by-
// short-id route to reconstruct environment/slug/display_name, which the
// API does not currently expose. Document this in the README rather than
// implementing a lossy import.
func (r *agentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"alterion_agent does not support terraform import: the Orion API has no GET-by-short-id route to reconstruct environment, slug, and display_name from a short id alone. Recreate the resource in configuration instead.",
	)
}
