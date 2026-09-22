package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// fakeConfigSchema is a real resource schema (two string attributes,
// "value" and "cloud_provider") used only to drive a validator.String
// directly in tests, without a full Terraform plan/apply cycle.
// cloudAccountIDValidator reads path.Root("cloud_provider") off the
// config for cross-field validation, so every fake config carries it.
var fakeConfigSchema = rschema.Schema{
	Attributes: map[string]rschema.Attribute{
		"value":          rschema.StringAttribute{Optional: true},
		"cloud_provider": rschema.StringAttribute{Optional: true},
	},
}

// runStringValidator drives a validator.String directly against a
// two-attribute fake config: "value" (the attribute under test) and
// "cloud_provider" (read by cloudAccountIDValidator as a sibling field).
func runStringValidator(t *testing.T, v validator.String, value string, cloudProvider string) validator.StringResponse {
	t.Helper()

	attrTypes := map[string]tftypes.Type{
		"value":          tftypes.String,
		"cloud_provider": tftypes.String,
	}
	attrValues := map[string]tftypes.Value{
		"value": tftypes.NewValue(tftypes.String, value),
	}
	if cloudProvider == "" {
		attrValues["cloud_provider"] = tftypes.NewValue(tftypes.String, nil)
	} else {
		attrValues["cloud_provider"] = tftypes.NewValue(tftypes.String, cloudProvider)
	}

	objType := tftypes.Object{AttributeTypes: attrTypes}
	objValue := tftypes.NewValue(objType, attrValues)

	rawConfig := tfsdk.Config{
		Raw:    objValue,
		Schema: fakeConfigSchema,
	}

	req := validator.StringRequest{
		Path:        path.Root("value"),
		ConfigValue: types.StringValue(value),
		Config:      rawConfig,
	}
	resp := &validator.StringResponse{}
	v.ValidateString(context.Background(), req, resp)
	return *resp
}

func TestWorkloadNameValidator_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"simple lowercase", "support-bot", false},
		{"underscore", "My_Agent", false},
		{"hyphen and digits", "billing-agent-2", false},
		{"single char", "a", false},
		{"max length 64", strings.Repeat("a", 64), false},
		{"over max length 65", strings.Repeat("a", 65), true},
		{"leading hyphen", "-bad-name", true},
		{"leading underscore", "_bad-name", true},
		{"contains space", "not valid", true},
		{"empty", "", true},
		{"unicode", "agentç", true},
		{"case sensitivity preserved not rejected", "AGENTCORE", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runStringValidator(t, workloadNameValidator{}, tc.value, "aws")
			gotErr := resp.Diagnostics.HasError()
			if gotErr != tc.wantErr {
				t.Errorf("workloadNameValidator(%q) error=%v, want error=%v (diags: %v)", tc.value, gotErr, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestCloudRegionValidator_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"aws standard", "us-east-1", false},
		{"aws govcloud", "us-gov-west-1", false},
		{"gcp region", "us-central1", false},
		{"azure region", "eastus", false},
		{"uppercase rejected", "US-EAST-1", true},
		{"spaces rejected", "us east 1", true},
		{"empty rejected", "", true},
		{"underscore rejected", "us_east_1", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runStringValidator(t, cloudRegionValidator{}, tc.value, "aws")
			gotErr := resp.Diagnostics.HasError()
			if gotErr != tc.wantErr {
				t.Errorf("cloudRegionValidator(%q) error=%v, want error=%v (diags: %v)", tc.value, gotErr, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestCloudAccountIDValidator_TableDriven(t *testing.T) {
	cases := []struct {
		name          string
		cloudProvider string
		value         string
		wantErr       bool
	}{
		// AWS: exactly 12 digits.
		{"aws valid", "aws", "123456789012", false},
		{"aws too short", "aws", "12345678901", true},
		{"aws too long", "aws", "1234567890123", true},
		{"aws non-numeric", "aws", "12345678901a", true},

		// GCP: 6-30 chars, lowercase letter start, lowercase/digit/hyphen,
		// no trailing hyphen (min/max length + trailing hyphen edge cases).
		{"gcp valid", "gcp", "my-project-123", false},
		{"gcp min length 6", "gcp", "ab1234", false},
		{"gcp below min length 5", "gcp", "ab123", true},
		{"gcp max length 30", "gcp", "a23456789012345678901234567890"[:30], false},
		{"gcp over max length 31", "gcp", "a234567890123456789012345678901", true},
		{"gcp trailing hyphen", "gcp", "my-project-", true},
		{"gcp uppercase rejected", "gcp", "My-Project-1", true},

		// Azure: GUID shape, including uppercase and braces (both rejected
		// here — the pattern is hex-lowercase-or-uppercase, no braces).
		{"azure valid lowercase", "azure", "11111111-1111-1111-1111-111111111111", false},
		{"azure valid uppercase hex", "azure", "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", false},
		{"azure with braces rejected", "azure", "{11111111-1111-1111-1111-111111111111}", true},
		{"azure missing segment", "azure", "11111111-1111-1111-1111", true},
		{"azure non-hex", "azure", "zzzzzzzz-1111-1111-1111-111111111111", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runStringValidator(t, cloudAccountIDValidator{}, tc.value, tc.cloudProvider)
			gotErr := resp.Diagnostics.HasError()
			if gotErr != tc.wantErr {
				t.Errorf("cloudAccountIDValidator(provider=%q, %q) error=%v, want error=%v (diags: %v)", tc.cloudProvider, tc.value, gotErr, tc.wantErr, resp.Diagnostics)
			}
		})
	}
}

func TestEnvironmentValidator_TableDriven(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"production", false},
		{"staging", false},
		{"development", false},
		{"prod", true},
		{"Production", true},
		{"", true},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			resp := runStringValidator(t, environmentValidator{}, tc.value, "aws")
			gotErr := resp.Diagnostics.HasError()
			if gotErr != tc.wantErr {
				t.Errorf("environmentValidator(%q) error=%v, want error=%v", tc.value, gotErr, tc.wantErr)
			}
		})
	}
}

func TestCloudProviderValidator_TableDriven(t *testing.T) {
	cases := []struct {
		value   string
		wantErr bool
	}{
		{"aws", false},
		{"gcp", false},
		{"azure", false},
		{"oracle-cloud", true},
		{"AWS", true},
		{"", true},
	}

	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			resp := runStringValidator(t, cloudProviderValidator{}, tc.value, "aws")
			gotErr := resp.Diagnostics.HasError()
			if gotErr != tc.wantErr {
				t.Errorf("cloudProviderValidator(%q) error=%v, want error=%v", tc.value, gotErr, tc.wantErr)
			}
		})
	}
}

func TestWorkloadResourceIDValidator_TableDriven(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{"aws arn", "arn:aws:bedrock-agentcore:us-east-1:123456789012:runtime/support-bot", false},
		{"gcp full resource name", "//run.googleapis.com/projects/my-project/locations/us-central1/services/billing-agent", false},
		{"azure resource id", "/subscriptions/11111111-1111-1111-1111-111111111111/resourceGroups/rg/providers/Microsoft.App/containerApps/my-app", false},
		{"empty rejected", "", true},
		{"exactly 2048 chars", strings.Repeat("a", 2048), false},
		{"over 2048 chars rejected", strings.Repeat("a", 2049), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := runStringValidator(t, workloadResourceIDValidator{}, tc.value, "aws")
			gotErr := resp.Diagnostics.HasError()
			if gotErr != tc.wantErr {
				t.Errorf("workloadResourceIDValidator(%q) error=%v, want error=%v", tc.name, gotErr, tc.wantErr)
			}
		})
	}
}
