package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlterionAI/terraform-provider-alterion/internal/client"
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
	client   *client.Client
	defaults providerDefaults
}

type agentResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	Environment          types.String `tfsdk:"environment"`
	CloudProvider        types.String `tfsdk:"cloud_provider"`
	CloudAccountID       types.String `tfsdk:"cloud_account_id"`
	CloudRegion          types.String `tfsdk:"cloud_region"`
	WorkloadName         types.String `tfsdk:"workload_name"`
	WorkloadResourceID   types.String `tfsdk:"workload_resource_id"`
	WorkloadType         types.String `tfsdk:"workload_type"`
	DisplayName          types.String `tfsdk:"display_name"`
	FunctionalBoundaries types.List   `tfsdk:"functional_boundaries"`
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
		Description: "Registers an asserted Orion agent. This is the register-late half of the compute-early/register-late pattern: create the runtime first (using the alterion_agent_path_key data source to compute its gateway URL ahead of time), then register it here with the workload's identifying details.",
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
			"cloud_provider": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "One of aws, gcp, azure. Falls back to the provider's cloud_provider, which defaults to aws. Changing this forces a new resource.",
				Validators: []validator.String{
					cloudProviderValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cloud_account_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Cloud account/project/subscription id the workload is deployed in: a 12-digit AWS account id, a GCP project id, or an Azure subscription GUID, matching cloud_provider. Falls back to the provider's cloud_account_id when omitted; one of the two must be set. Changing this forces a new resource.",
				Validators: []validator.String{
					cloudAccountIDValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"cloud_region": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Cloud region the workload is deployed in, e.g. us-east-1. Falls back to the provider's cloud_region when omitted; one of the two must be set. Changing this forces a new resource.",
				Validators: []validator.String{
					cloudRegionValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workload_name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the workload (e.g. an AWS Bedrock AgentCore agent_runtime_name, a GCP Cloud Run service name, an ECS service name). Case-sensitive; must match ^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$. Required: the identity must exist before the runtime does. Changing this forces a new resource.",
				Validators: []validator.String{
					workloadNameValidator{},
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"workload_resource_id": schema.StringAttribute{
				Optional:    true,
				Description: "The workload's own post-create unique id once it exists: an AWS ARN, a GCP full resource name, or an Azure resource id.",
				Validators: []validator.String{
					workloadResourceIDValidator{},
				},
			},
			"workload_type": schema.StringAttribute{
				Optional:    true,
				Description: "Override for the workload's type, e.g. bedrock-agentcore-runtime, ecs-service, cloud-run-service. When omitted, the server infers it from workload_resource_id.",
			},
			"display_name": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Human-readable display name for the agent. Defaults to workload_name when omitted.",
				PlanModifiers: []planmodifier.String{
					defaultToWorkloadNameModifier{},
				},
			},
			"functional_boundaries": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Functional Orion boundaries the agent also joins. The environment boundary is derived from environment and never listed here.",
			},
			"adopt": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "When true, allows this resource to take over an agent row already owned by a different principal (including an organically header-asserted agent with no owner, which otherwise 409s on registration), and to delete it even if owned elsewhere. Defaults to false. Requires the provider's api_token to have been minted with adopt permission (allowAdopt: true, scope agents:automation:adopt); otherwise the API returns 403, which this provider surfaces as an error.",
			},
			"agent_id": schema.StringAttribute{
				Computed:    true,
				Description: "The full asserted agent id, of the form asserted|<environment>|<identity key derived from cloud_provider, cloud_account_id, cloud_region, workload_name>.",
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
	r.defaults = providerDefaults{
		CloudProvider:  data.CloudProvider,
		CloudAccountID: data.CloudAccountID,
		CloudRegion:    data.CloudRegion,
	}
}

func (r *agentResource) identity(model *agentResourceModel, diags *diag.Diagnostics) client.WorkloadIdentity {
	identity, idDiags := resolveIdentity(model.CloudProvider, model.CloudAccountID, model.CloudRegion, model.WorkloadName, r.defaults)
	diags.Append(idDiags...)
	return identity
}

func (r *agentResource) createOrUpdate(ctx context.Context, model *agentResourceModel, diags *diag.Diagnostics) {
	identity := r.identity(model, diags)
	if diags.HasError() {
		return
	}

	var functionalBoundaries []string
	diags.Append(model.FunctionalBoundaries.ElementsAs(ctx, &functionalBoundaries, true)...)
	if diags.HasError() {
		return
	}

	req := client.CreateAgentRequest{
		Environment:          model.Environment.ValueString(),
		CloudProvider:        identity.CloudProvider,
		CloudAccountID:       identity.CloudAccountID,
		CloudRegion:          identity.CloudRegion,
		WorkloadName:         identity.WorkloadName,
		WorkloadResourceID:   model.WorkloadResourceID.ValueString(),
		WorkloadType:         model.WorkloadType.ValueString(),
		DisplayName:          model.DisplayName.ValueString(),
		FunctionalBoundaries: functionalBoundaries,
		Adopt:                model.Adopt.ValueBool(),
	}

	result, err := r.client.CreateAgent(ctx, req)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 409 {
			diags.AddError(
				"Agent Owned By Another Principal",
				fmt.Sprintf("The Orion API reports environment=%q %s is owned by a different principal: %s. Set adopt = true to take ownership.",
					req.Environment, identitySummary(identity), apiErr.Message),
			)
			return
		}
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Code == "ASSERTED_WORKLOAD_BOUNDARY_NOT_FOUND" {
			diags.AddError(
				"Functional Boundary Not Found",
				fmt.Sprintf("The Orion API could not find functional boundary %q for environment=%q %s: %s",
					apiErr.BoundaryName, req.Environment, identitySummary(identity), apiErr.Message),
			)
			return
		}
		diags.AddError(
			"Error Creating/Updating Agent",
			fmt.Sprintf("Could not create/update agent environment=%q %s: %s", req.Environment, identitySummary(identity), err),
		)
		return
	}

	model.CloudProvider = types.StringValue(identity.CloudProvider)
	model.CloudAccountID = types.StringValue(identity.CloudAccountID)
	model.CloudRegion = types.StringValue(identity.CloudRegion)
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

// Read refreshes state via GET /api/v1/agents/asserted/{shortId}. Identity
// attributes are kept from state (the server doesn't echo them back). If
// state has no short_id yet, it's derived via the path-key lookup first.
func (r *agentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var model agentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	environment := model.Environment.ValueString()
	identity := r.identity(&model, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	shortID := model.ShortID.ValueString()

	if shortID == "" {
		pathKey, err := r.client.GetPathKey(ctx, environment, identity)
		if err != nil {
			if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 404 {
				resp.State.RemoveResource(ctx)
				return
			}
			resp.Diagnostics.AddError(
				"Error Reading Agent",
				fmt.Sprintf("Could not resolve short id for agent environment=%q %s: %s", environment, identitySummary(identity), err),
			)
			return
		}
		shortID = pathKey.ShortID
	}

	result, err := r.client.GetAgent(ctx, shortID)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 404 {
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
			return
		}
		resp.Diagnostics.AddError(
			"Error Deleting Agent",
			fmt.Sprintf("Could not delete agent short_id=%q: %s", shortID, err),
		)
		return
	}
}

// ImportState is unsupported: the API has no GET-by-short-id route to
// reconstruct the resource's other attributes from a short id alone.
func (r *agentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.AddError(
		"Import Not Supported",
		"alterion_agent does not support terraform import: the Orion API has no GET-by-short-id route to reconstruct environment, cloud_provider, cloud_account_id, cloud_region, workload_name, and display_name from a short id alone. Recreate the resource in configuration instead.",
	)
}

// workloadResourceIDValidator enforces non-empty, at most 2048 characters,
// with no cloud-specific shape validation — the server owns that.
type workloadResourceIDValidator struct{}

func (v workloadResourceIDValidator) Description(_ context.Context) string {
	return "value must be non-empty and at most 2048 characters"
}

func (v workloadResourceIDValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v workloadResourceIDValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	if len(value) == 0 || len(value) > 2048 {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Workload Resource Id",
			fmt.Sprintf("workload_resource_id must be non-empty and at most 2048 characters, got %d", len(value)),
		)
	}
}

// defaultToWorkloadNameModifier defaults display_name to workload_name
// when display_name is omitted (a sibling-derived default needs a plan
// modifier; schema Default only supports static values).
type defaultToWorkloadNameModifier struct{}

func (m defaultToWorkloadNameModifier) Description(_ context.Context) string {
	return "Defaults display_name to workload_name when display_name is not set in configuration."
}

func (m defaultToWorkloadNameModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m defaultToWorkloadNameModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}

	var workloadName types.String
	diags := req.Config.GetAttribute(ctx, path.Root("workload_name"), &workloadName)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() || workloadName.IsNull() || workloadName.IsUnknown() {
		return
	}

	resp.PlanValue = workloadName
}
