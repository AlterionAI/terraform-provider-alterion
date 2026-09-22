package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/AlterionAI/terraform-provider-alterion/internal/client"
)

// providerDefaults is the subset of provider-level config that identity
// attributes fall back to when a resource/data source omits them.
type providerDefaults struct {
	CloudProvider  string
	CloudAccountID string
	CloudRegion    string
}

// resolveIdentity fills cloud_provider/cloud_account_id/cloud_region from
// the provider's defaults when omitted from config, and errors if
// cloud_account_id or cloud_region end up unset either way. cloud_provider
// always resolves since the provider's own default is "aws".
func resolveIdentity(cloudProvider, cloudAccountID, cloudRegion, workloadName types.String, defaults providerDefaults) (client.WorkloadIdentity, diag.Diagnostics) {
	var diags diag.Diagnostics

	identity := client.WorkloadIdentity{
		CloudProvider:  firstNonEmpty(cloudProvider.ValueString(), defaults.CloudProvider),
		CloudAccountID: firstNonEmpty(cloudAccountID.ValueString(), defaults.CloudAccountID),
		CloudRegion:    firstNonEmpty(cloudRegion.ValueString(), defaults.CloudRegion),
		WorkloadName:   workloadName.ValueString(),
	}

	if identity.CloudAccountID == "" {
		diags.AddError("Missing Cloud Account Id", "cloud_account_id must be set here or as a default via the provider's cloud_account_id.")
	}
	if identity.CloudRegion == "" {
		diags.AddError("Missing Cloud Region", "cloud_region must be set here or as a default via the provider's cloud_region.")
	}

	return identity, diags
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
