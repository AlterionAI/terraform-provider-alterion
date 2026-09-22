package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlterionAI/terraform-provider-alterion/internal/client"
)

var (
	_ datasource.DataSource              = &agentPathKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &agentPathKeyDataSource{}
)

var validEnvironments = []string{"production", "staging", "development"}

// validCloudProviders enumerates the clouds cloud_provider accepts.
var validCloudProviders = []string{"aws", "gcp", "azure"}

// cloudAccountIDPatterns validates cloud_account_id's shape per
// cloud_provider: 12-digit AWS account id, GCP project id, Azure GUID.
var cloudAccountIDPatterns = map[string]*regexp.Regexp{
	"aws":   regexp.MustCompile(`^\d{12}$`),
	"gcp":   regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`),
	"azure": regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`),
}

// cloudRegionPattern is cloud-agnostic: lowercase alphanumerics and
// hyphens (e.g. us-east-1, us-central1, eastus).
var cloudRegionPattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// workloadNamePattern is cloud-agnostic: starts with an alphanumeric,
// then alphanumerics/underscores/hyphens, up to 64 characters.
// Case-sensitive — no lowercasing or folding.
var workloadNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func NewAgentPathKeyDataSource() datasource.DataSource {
	return &agentPathKeyDataSource{}
}

type agentPathKeyDataSource struct {
	client     *client.Client
	gatewayURL string
	defaults   providerDefaults
}

type agentPathKeyDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Environment    types.String `tfsdk:"environment"`
	CloudProvider  types.String `tfsdk:"cloud_provider"`
	CloudAccountID types.String `tfsdk:"cloud_account_id"`
	CloudRegion    types.String `tfsdk:"cloud_region"`
	WorkloadName   types.String `tfsdk:"workload_name"`
	AgentID        types.String `tfsdk:"agent_id"`
	ShortID        types.String `tfsdk:"short_id"`
	PathPrefix     types.String `tfsdk:"path_prefix"`
	GatewayBaseURL types.String `tfsdk:"gateway_base_url"`
}

func (d *agentPathKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_agent_path_key"
}

func (d *agentPathKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Resolves the deterministic gateway path key for an asserted Orion agent, ahead of the agent's runtime existing or being registered. Use this to configure a runtime's outbound base URL(s) before the runtime itself is created.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Same value as agent_id.",
			},
			"environment": schema.StringAttribute{
				Required:    true,
				Description: "One of production, staging, development.",
				Validators: []validator.String{
					environmentValidator{},
				},
			},
			"cloud_provider": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "One of aws, gcp, azure. Defaults to aws.",
				Validators: []validator.String{
					cloudProviderValidator{},
				},
			},
			"cloud_account_id": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Cloud account/project/subscription id the workload is (or will be) deployed in: a 12-digit AWS account id, a GCP project id, or an Azure subscription GUID, matching cloud_provider. Falls back to the provider's cloud_account_id when omitted; one of the two must be set.",
				Validators: []validator.String{
					cloudAccountIDValidator{},
				},
			},
			"cloud_region": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Cloud region the workload is (or will be) deployed in, e.g. us-east-1. Falls back to the provider's cloud_region when omitted; one of the two must be set.",
				Validators: []validator.String{
					cloudRegionValidator{},
				},
			},
			"workload_name": schema.StringAttribute{
				Required:    true,
				Description: "Name of the workload (e.g. an AWS Bedrock AgentCore agent_runtime_name, a GCP Cloud Run service name, an ECS service name). Case-sensitive; must match ^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$.",
				Validators: []validator.String{
					workloadNameValidator{},
				},
			},
			"agent_id": schema.StringAttribute{
				Computed:    true,
				Description: "The full asserted agent id, of the form asserted|<environment>|<identity key derived from cloud_provider, cloud_account_id, cloud_region, workload_name>.",
			},
			"short_id": schema.StringAttribute{
				Computed:    true,
				Description: "12 hex character short id derived from the agent id, used in gateway paths.",
			},
			"path_prefix": schema.StringAttribute{
				Computed:    true,
				Description: "Gateway path prefix segment (currently always \"a\").",
			},
			"gateway_base_url": schema.StringAttribute{
				Computed:    true,
				Description: "Complete base URL the agent must use for LLM calls. Set your runtime's SDK base-URL environment variable to this value. Always a full URL: populated from the server response's gatewayBaseUrl when present, otherwise composed from the provider's gateway_url + path_prefix + short_id.",
			},
		},
	}
}

func (d *agentPathKeyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	data, ok := req.ProviderData.(*AlterionProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Data Source Configure Type",
			fmt.Sprintf("Expected *provider.AlterionProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	d.client = data.Client
	d.gatewayURL = data.GatewayURL
	d.defaults = providerDefaults{
		CloudProvider:  data.CloudProvider,
		CloudAccountID: data.CloudAccountID,
		CloudRegion:    data.CloudRegion,
	}
}

func (d *agentPathKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model agentPathKeyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	environment := model.Environment.ValueString()
	identity, diags := resolveIdentity(model.CloudProvider, model.CloudAccountID, model.CloudRegion, model.WorkloadName, d.defaults)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	result, err := d.client.GetPathKey(ctx, environment, identity)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 409 {
			resp.Diagnostics.AddError(
				"Short Id Collision",
				fmt.Sprintf("The Orion API reported a short-id collision for environment=%q %s: %s (existing agent id: %s)",
					environment, identitySummary(identity), apiErr.Message, apiErr.ExistingAgentID),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Agent Path Key",
			fmt.Sprintf("Could not read path key for environment=%q %s: %s", environment, identitySummary(identity), err),
		)
		return
	}

	if expected := client.ExpectedShortID(environment, client.IdentityKey(identity)); expected != result.ShortID {
		resp.Diagnostics.AddWarning(
			"Short Id Mismatch",
			fmt.Sprintf("The server returned short_id %q for environment=%q %s, but the locally expected value is %q. The server's value is authoritative and is what this data source uses; this warning only flags a possible drift in the derivation formula.",
				result.ShortID, environment, identitySummary(identity), expected),
		)
	}

	gatewayBaseURL := result.GatewayBaseURL
	if gatewayBaseURL == "" && d.gatewayURL != "" {
		gatewayBaseURL = strings.TrimRight(d.gatewayURL, "/") + "/" + result.PathPrefix + "/" + result.ShortID
	}

	model.CloudProvider = types.StringValue(identity.CloudProvider)
	model.CloudAccountID = types.StringValue(identity.CloudAccountID)
	model.CloudRegion = types.StringValue(identity.CloudRegion)
	model.ID = types.StringValue(result.AgentID)
	model.AgentID = types.StringValue(result.AgentID)
	model.ShortID = types.StringValue(result.ShortID)
	model.PathPrefix = types.StringValue(result.PathPrefix)
	model.GatewayBaseURL = types.StringValue(gatewayBaseURL)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// defaultCloudProvider returns "aws" when v is unset.
func defaultCloudProvider(v types.String) string {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return "aws"
	}
	return v.ValueString()
}

func identitySummary(identity client.WorkloadIdentity) string {
	return fmt.Sprintf("cloud_provider=%q cloud_account_id=%q cloud_region=%q workload_name=%q",
		identity.CloudProvider, identity.CloudAccountID, identity.CloudRegion, identity.WorkloadName)
}

// environmentValidator enforces the environment enum client-side.
type environmentValidator struct{}

func (v environmentValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be one of: %s", strings.Join(validEnvironments, ", "))
}

func (v environmentValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v environmentValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	for _, valid := range validEnvironments {
		if value == valid {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid Environment",
		fmt.Sprintf("environment must be one of %s, got %q", strings.Join(validEnvironments, ", "), value),
	)
}

// cloudProviderValidator enforces the cloud_provider enum client-side.
type cloudProviderValidator struct{}

func (v cloudProviderValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must be one of: %s", strings.Join(validCloudProviders, ", "))
}

func (v cloudProviderValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v cloudProviderValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	for _, valid := range validCloudProviders {
		if value == valid {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid Cloud Provider",
		fmt.Sprintf("cloud_provider must be one of %s, got %q", strings.Join(validCloudProviders, ", "), value),
	)
}

// cloudAccountIDValidator enforces cloud_account_id's format against the
// cloud_provider configured alongside it (defaults to aws).
type cloudAccountIDValidator struct{}

func (v cloudAccountIDValidator) Description(_ context.Context) string {
	return "value must be a valid account/project/subscription id for the configured cloud_provider"
}

func (v cloudAccountIDValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v cloudAccountIDValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()

	var cloudProviderValue types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("cloud_provider"), &cloudProviderValue)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cloudProviderValue.IsUnknown() {
		return
	}
	cloudProvider := defaultCloudProvider(cloudProviderValue)

	pattern, ok := cloudAccountIDPatterns[cloudProvider]
	if !ok {
		// cloudProviderValidator reports an invalid cloud_provider.
		return
	}
	if !pattern.MatchString(value) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Cloud Account Id",
			fmt.Sprintf("cloud_account_id %q must match %s for cloud_provider %q", value, pattern.String(), cloudProvider),
		)
	}
}

// cloudRegionValidator enforces the cloud region name format client-side.
type cloudRegionValidator struct{}

func (v cloudRegionValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must match %s", cloudRegionPattern.String())
}

func (v cloudRegionValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v cloudRegionValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	if !cloudRegionPattern.MatchString(value) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Cloud Region",
			fmt.Sprintf("cloud_region %q must match %s (e.g. us-east-1)", value, cloudRegionPattern.String()),
		)
	}
}

// workloadNameValidator enforces the cloud-agnostic workload name format
// client-side. Case-sensitive: no lowercasing or folding is applied here
// or anywhere downstream.
type workloadNameValidator struct{}

func (v workloadNameValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must match %s", workloadNamePattern.String())
}

func (v workloadNameValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v workloadNameValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	if !workloadNamePattern.MatchString(value) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Workload Name",
			fmt.Sprintf("workload_name %q must match %s (case-sensitive; starts with an alphanumeric, then letters/digits/underscores/hyphens, up to 64 characters)", value, workloadNamePattern.String()),
		)
	}
}
