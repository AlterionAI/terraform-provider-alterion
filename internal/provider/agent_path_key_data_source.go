package provider

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlterionAI/terraform-provider-alterion/internal/client"
)

var (
	_ datasource.DataSource              = &agentPathKeyDataSource{}
	_ datasource.DataSourceWithConfigure = &agentPathKeyDataSource{}
)

// slugPattern mirrors the server-side validation: lowercase alphanumerics
// and hyphens, no leading/trailing/doubled hyphens, max 64 chars.
var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

const maxSlugLength = 64

var validEnvironments = []string{"production", "staging", "development"}

func NewAgentPathKeyDataSource() datasource.DataSource {
	return &agentPathKeyDataSource{}
}

type agentPathKeyDataSource struct {
	client     *client.Client
	gatewayURL string
}

type agentPathKeyDataSourceModel struct {
	ID             types.String `tfsdk:"id"`
	Environment    types.String `tfsdk:"environment"`
	Slug           types.String `tfsdk:"slug"`
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
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "Stable slug identifying the agent within the environment, e.g. \"<aws-account-id>-<region>-<runtime-name>\". Must match ^[a-z0-9]+(?:-[a-z0-9]+)*$ and be at most 64 characters.",
				Validators: []validator.String{
					slugValidator{},
				},
			},
			"agent_id": schema.StringAttribute{
				Computed:    true,
				Description: "The full asserted agent id, of the form asserted|<environment>|<slug>.",
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
				Description: "Full gateway base URL for this agent. Populated from the server response, or computed locally from the provider's gateway_url + path_prefix + short_id when the server returns null and gateway_url is configured.",
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
}

func (d *agentPathKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model agentPathKeyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	environment := model.Environment.ValueString()
	slug := model.Slug.ValueString()

	result, err := d.client.GetPathKey(ctx, environment, slug)
	if err != nil {
		if apiErr, ok := err.(*client.APIError); ok && apiErr.Status == 409 {
			resp.Diagnostics.AddError(
				"Short Id Collision",
				fmt.Sprintf("The Orion API reported a short-id collision for environment=%q slug=%q: %s (existing agent id: %s)",
					environment, slug, apiErr.Message, apiErr.ExistingAgentID),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Agent Path Key",
			fmt.Sprintf("Could not read path key for environment=%q slug=%q: %s", environment, slug, err),
		)
		return
	}

	if expected := client.ExpectedShortID(environment, slug); expected != result.ShortID {
		resp.Diagnostics.AddWarning(
			"Short Id Mismatch",
			fmt.Sprintf("The server returned short_id %q for environment=%q slug=%q, but the locally expected value is %q. The server's value is authoritative and is what this data source uses; this warning only flags a possible drift in the derivation formula.",
				result.ShortID, environment, slug, expected),
		)
	}

	gatewayBaseURL := result.GatewayBaseURL
	if gatewayBaseURL == "" && d.gatewayURL != "" {
		gatewayBaseURL = strings.TrimRight(d.gatewayURL, "/") + "/" + result.PathPrefix + "/" + result.ShortID
	}

	model.ID = types.StringValue(result.AgentID)
	model.AgentID = types.StringValue(result.AgentID)
	model.ShortID = types.StringValue(result.ShortID)
	model.PathPrefix = types.StringValue(result.PathPrefix)
	model.GatewayBaseURL = types.StringValue(gatewayBaseURL)

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// environmentValidator enforces the environment enum client-side, so
// `terraform plan` fails fast instead of round-tripping to the API for an
// obviously invalid value.
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

// slugValidator enforces the slug regex and length limit client-side.
type slugValidator struct{}

func (v slugValidator) Description(_ context.Context) string {
	return fmt.Sprintf("value must match %s and be at most %d characters", slugPattern.String(), maxSlugLength)
}

func (v slugValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v slugValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	value := req.ConfigValue.ValueString()
	if len(value) == 0 || len(value) > maxSlugLength {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Slug Length",
			fmt.Sprintf("slug must be between 1 and %d characters, got %d", maxSlugLength, len(value)),
		)
		return
	}
	if !slugPattern.MatchString(value) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid Slug Format",
			fmt.Sprintf("slug %q must match %s (lowercase alphanumeric segments separated by single hyphens)", value, slugPattern.String()),
		)
	}
}
