// Package provider implements the Terraform provider for Alterion Orion:
// registering AI agent runtimes ("asserted agents") and resolving their
// gateway path keys ahead of runtime deployment.
package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlterionAI/terraform-provider-alterion/internal/client"
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
	OrionURL       types.String `tfsdk:"orion_url"`
	APIToken       types.String `tfsdk:"api_token"`
	GatewayURL     types.String `tfsdk:"gateway_url"`
	CloudProvider  types.String `tfsdk:"cloud_provider"`
	CloudAccountID types.String `tfsdk:"cloud_account_id"`
	CloudRegion    types.String `tfsdk:"cloud_region"`
}

// AlterionProviderData is passed to resources/data sources via
// resp.ResourceData / resp.DataSourceData.
type AlterionProviderData struct {
	Client         *client.Client
	GatewayURL     string // required; validated non-empty absolute URL in Configure
	CloudProvider  string // defaults to "aws"; see defaultCloudProvider
	CloudAccountID string // optional; empty when unset
	CloudRegion    string // optional; empty when unset
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
				Optional:    true,
				Description: "Base URL of the Orion web app (e.g. https://orion.example.com). May also be set via the ALTERION_ORION_URL environment variable.",
			},
			"api_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Orion API token (looks like orion_at_<hex>). May also be set via the ALTERION_API_TOKEN environment variable. Ownership is tied to the Orion user who minted the token, not the token itself, so rotating it keeps ownership intact. Tokens expire (90 days by default); a rejected token surfaces as a 401.",
			},
			"gateway_url": schema.StringAttribute{
				Optional:    true,
				Description: "Public base URL of the Orion AI gateway, e.g. https://gw.example.com. Required: the data source's gateway_base_url is built from it and must be injected into the agent runtime's environment. May also be set via the ALTERION_GATEWAY_URL environment variable. Must be an absolute https:// (or http:// for local use) URL with no path.",
			},
			"cloud_provider": schema.StringAttribute{
				Optional:    true,
				Description: "Default cloud_provider for the alterion_agent resource and alterion_agent_path_key data source when they omit it. One of aws, gcp, azure. Defaults to aws.",
				Validators: []validator.String{
					cloudProviderValidator{},
				},
			},
			"cloud_account_id": schema.StringAttribute{
				Optional:    true,
				Description: "Default cloud_account_id for the resource and data source when they omit it.",
			},
			"cloud_region": schema.StringAttribute{
				Optional:    true,
				Description: "Default cloud_region for the resource and data source when they omit it.",
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

	gatewayURL := config.GatewayURL.ValueString()
	if gatewayURL == "" {
		gatewayURL = os.Getenv("ALTERION_GATEWAY_URL")
	}
	if gatewayURL == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("gateway_url"),
			"Missing Gateway URL",
			"The provider requires a gateway_url, set either in the provider configuration block or via the ALTERION_GATEWAY_URL environment variable. It is the public base URL of the Orion AI gateway that gateway_base_url is composed from and must be injected into the agent runtime's environment.",
		)
	} else if err := validateGatewayURL(gatewayURL); err != nil {
		resp.Diagnostics.AddAttributeError(
			path.Root("gateway_url"),
			"Invalid Gateway URL",
			fmt.Sprintf("gateway_url %q is invalid: %s", gatewayURL, err),
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	client.Version = p.version
	c := client.New(orionURL, apiToken)

	data := &AlterionProviderData{
		Client:         c,
		GatewayURL:     gatewayURL,
		CloudProvider:  defaultCloudProvider(config.CloudProvider),
		CloudAccountID: config.CloudAccountID.ValueString(),
		CloudRegion:    config.CloudRegion.ValueString(),
	}

	resp.DataSourceData = data
	resp.ResourceData = data
}

// validateGatewayURL requires an absolute http(s) URL with no path, query,
// or fragment — just scheme + host, so it can safely have "/<path_prefix>/
// <short_id>" appended.
func validateGatewayURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("not a valid URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("scheme must be https:// (or http:// for local use), got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("must include a host")
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("must not include a path, got %q", u.Path)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("must not include a query or fragment")
	}
	return nil
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
