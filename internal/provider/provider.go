// Package provider implements the Terraform provider for Alterion Orion:
// registering AI agent runtimes ("asserted agents") and resolving their
// gateway path keys ahead of runtime deployment.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlterionAI/alterion-terraform-provider/internal/client"
)

// Ensure AlterionProvider satisfies the provider.Provider interface.
var _ provider.Provider = &AlterionProvider{}

// AlterionProvider is the provider implementation.
type AlterionProvider struct {
	// version is set by the goreleaser build process (or "dev" locally) via
	// main.go, and reported in the client User-Agent header.
	version string
}

// alterionProviderModel maps the provider configuration block.
type alterionProviderModel struct {
	OrionURL   types.String `tfsdk:"orion_url"`
	APIToken   types.String `tfsdk:"api_token"`
	GatewayURL types.String `tfsdk:"gateway_url"`
}

// AlterionProviderData is passed to resources/data sources via
// resp.ResourceData / resp.DataSourceData.
type AlterionProviderData struct {
	Client     *client.Client
	GatewayURL string // optional; empty when unset
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &AlterionProvider{version: version}
	}
}

func (p *AlterionProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "alterion"
	resp.Version = p.version
}

func (p *AlterionProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages Alterion Orion asserted agent registrations and resolves their gateway path keys ahead of runtime deployment.",
		Attributes: map[string]schema.Attribute{
			"orion_url": schema.StringAttribute{
				Required:    true,
				Description: "Base URL of the Orion web app (e.g. https://orion.example.com). May also be set via the ALTERION_ORION_URL environment variable.",
			},
			"api_token": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Orion API token (looks like orion_at_<hex>). May also be set via the ALTERION_API_TOKEN environment variable. Ownership of anything this token registers is tied to the Orion user who minted it, not to the token itself, so rotating the token does not change ownership and a re-apply keeps working after rotation. Tokens expire (90 days by default) and stop working if the minting user loses the approver role; a 401 from the API surfaces as \"token rejected; mint a new one\".",
			},
			"gateway_url": schema.StringAttribute{
				Optional:    true,
				Description: "Optional base URL for the Alterion gateway. When set, the alterion_agent_path_key data source computes gateway_base_url as \"<gateway_url>/<path_prefix>/<short_id>\" if the server did not return one.",
			},
		},
	}
}

func (p *AlterionProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config alterionProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	orionURL := config.OrionURL.ValueString()
	if orionURL == "" {
		orionURL = os.Getenv("ALTERION_ORION_URL")
	}
	if orionURL == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("orion_url"),
			"Missing Orion URL",
			"The provider requires an orion_url, set either in the provider configuration block or via the ALTERION_ORION_URL environment variable.",
		)
	}

	apiToken := config.APIToken.ValueString()
	if apiToken == "" {
		apiToken = os.Getenv("ALTERION_API_TOKEN")
	}
	if apiToken == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_token"),
			"Missing API Token",
			"The provider requires an api_token, set either in the provider configuration block or via the ALTERION_API_TOKEN environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	client.Version = p.version
	c := client.New(orionURL, apiToken)

	data := &AlterionProviderData{
		Client:     c,
		GatewayURL: config.GatewayURL.ValueString(),
	}

	resp.DataSourceData = data
	resp.ResourceData = data
}

func (p *AlterionProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAgentResource,
	}
}

func (p *AlterionProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAgentPathKeyDataSource,
	}
}
