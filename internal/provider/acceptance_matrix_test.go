package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"

	"github.com/AlterionAI/terraform-provider-alterion/internal/client"
)

// cloudFixture is one cloud provider's worth of identity values, used to
// run the same lifecycle/replace/update assertions across aws/gcp/azure
// without repeating the config three times.
type cloudFixture struct {
	provider  string
	accountID string
	region    string
	name      string
}

var cloudFixtures = []cloudFixture{
	{provider: "aws", accountID: "123456789012", region: "us-east-1", name: "SupportBot"},
	{provider: "gcp", accountID: "my-gcp-project-1", region: "us-central1", name: "BillingAgent"},
	{provider: "azure", accountID: "11111111-1111-1111-1111-111111111111", region: "eastus", name: "InvoiceAgent"},
}

func agentConfig(baseURL string, f cloudFixture, displayName string) string {
	return providerConfig(baseURL) + fmt.Sprintf(`
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = %q
  cloud_account_id = %q
  cloud_region     = %q
  workload_name    = %q
  display_name     = %q
}
`, f.provider, f.accountID, f.region, f.name, displayName)
}

// TestAccAgentResource_LifecycleMatrix runs the create/read/update/destroy
// lifecycle once per supported cloud provider (aws/gcp/azure), each against
// its own fake server instance and its own cloud-shaped identity fixture.
func TestAccAgentResource_LifecycleMatrix(t *testing.T) {
	for _, f := range cloudFixtures {
		t.Run(f.provider, func(t *testing.T) {
			server := newFakeOrionServer(t)
			defer server.Close()

			identity := client.WorkloadIdentity{CloudProvider: f.provider, CloudAccountID: f.accountID, CloudRegion: f.region, WorkloadName: f.name}
			wantShortID := client.ExpectedShortID("staging", client.IdentityKey(identity))

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: agentConfig(server.URL, f, "v1"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("alterion_agent.this", "cloud_provider", f.provider),
							resource.TestCheckResourceAttr("alterion_agent.this", "cloud_account_id", f.accountID),
							resource.TestCheckResourceAttr("alterion_agent.this", "cloud_region", f.region),
							resource.TestCheckResourceAttr("alterion_agent.this", "short_id", wantShortID),
							resource.TestCheckResourceAttr("alterion_agent.this", "status", "active"),
							resource.TestCheckResourceAttr("alterion_agent.this", "is_registered", "true"),
						),
					},
					{
						// In-place update: display_name changes, identity
						// doesn't, so short_id must be stable.
						Config: agentConfig(server.URL, f, "v2"),
						Check: resource.ComposeAggregateTestCheckFunc(
							resource.TestCheckResourceAttr("alterion_agent.this", "display_name", "v2"),
							resource.TestCheckResourceAttr("alterion_agent.this", "short_id", wantShortID),
						),
					},
				},
			})
		})
	}
}

// TestAccAgentResource_RequiresReplace proves each identity attribute
// forces replacement when changed, and none of them trigger replacement
// when the other, non-identity attributes change instead.
func TestAccAgentResource_RequiresReplace(t *testing.T) {
	base := cloudFixture{provider: "aws", accountID: "123456789012", region: "us-east-1", name: "ClaimsReview"}

	cases := []struct {
		name    string
		mutate  func(f cloudFixture) cloudFixture
		envStep string // non-empty overrides environment on step 2
	}{
		{name: "environment", envStep: "production"},
		{name: "cloud_provider", mutate: func(f cloudFixture) cloudFixture {
			f.provider, f.accountID, f.region = "gcp", "my-gcp-project-1", "us-central1"
			return f
		}},
		{name: "cloud_account_id", mutate: func(f cloudFixture) cloudFixture { f.accountID = "999999999999"; return f }},
		{name: "cloud_region", mutate: func(f cloudFixture) cloudFixture { f.region = "us-west-2"; return f }},
		{name: "workload_name", mutate: func(f cloudFixture) cloudFixture { f.name = "OtherWorkload"; return f }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newFakeOrionServer(t)
			defer server.Close()

			step1Config := agentConfig(server.URL, base, "v1")
			var step2Config string
			if tc.envStep != "" {
				step2Config = providerConfig(server.URL) + fmt.Sprintf(`
resource "alterion_agent" "this" {
  environment      = %q
  cloud_provider   = %q
  cloud_account_id = %q
  cloud_region     = %q
  workload_name    = %q
  display_name     = "v1"
}
`, tc.envStep, base.provider, base.accountID, base.region, base.name)
			} else {
				mutated := tc.mutate(base)
				step2Config = agentConfig(server.URL, mutated, "v1")
			}

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{Config: step1Config},
					{
						Config: step2Config,
						ConfigPlanChecks: resource.ConfigPlanChecks{
							PreApply: []plancheck.PlanCheck{
								plancheck.ExpectResourceAction("alterion_agent.this", plancheck.ResourceActionReplace),
							},
						},
					},
				},
			})
		})
	}
}

// TestAccAgentResource_InPlaceUpdates proves display_name,
// functional_boundaries, workload_resource_id, and workload_type all
// update in place (no replace) via a re-POST.
func TestAccAgentResource_InPlaceUpdates(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	step1 := providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment            = "staging"
  cloud_provider         = "aws"
  cloud_account_id       = "123456789012"
  cloud_region           = "us-east-1"
  workload_name          = "ClaimsReview"
  display_name           = "v1"
  workload_resource_id   = "arn:aws:bedrock-agentcore:us-east-1:123456789012:runtime/v1"
  workload_type          = "bedrock-agentcore-runtime"
  functional_boundaries  = ["finance"]
}
`
	step2 := providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment            = "staging"
  cloud_provider         = "aws"
  cloud_account_id       = "123456789012"
  cloud_region           = "us-east-1"
  workload_name          = "ClaimsReview"
  display_name           = "v2"
  workload_resource_id   = "arn:aws:bedrock-agentcore:us-east-1:123456789012:runtime/v2"
  workload_type          = "ecs-service"
  functional_boundaries  = ["finance", "production-support"]
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: step1},
			{
				Config: step2,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("alterion_agent.this", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "display_name", "v2"),
					resource.TestCheckResourceAttr("alterion_agent.this", "workload_resource_id", "arn:aws:bedrock-agentcore:us-east-1:123456789012:runtime/v2"),
					resource.TestCheckResourceAttr("alterion_agent.this", "workload_type", "ecs-service"),
					resource.TestCheckResourceAttr("alterion_agent.this", "functional_boundaries.#", "2"),
				),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_MissingCloudRegion mirrors the account-id
// case: omitting cloud_region at both levels errors.
func TestAccAgentPathKeyDataSource_MissingCloudRegion(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_account_id = "123456789012"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Missing Cloud Region`),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_ShortIDCollision proves a 409 from the
// path-key route surfaces the existing agent id in the error.
func TestAccAgentPathKeyDataSource_ShortIDCollision(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":         false,
			"error":           "short id collision",
			"code":            "ASSERTED_WORKLOAD_SHORT_ID_COLLISION",
			"existingAgentId": "asserted|staging|aws.123456789012.us-east-1.other-workload",
		})
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Short Id Collision`),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_FlagOff404 proves a route-level 404 (the
// asserted-agent feature disabled server-side) surfaces a clear error
// rather than a confusing decode failure.
func TestAccAgentPathKeyDataSource_FlagOff404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "not found"})
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Error Reading Agent Path Key`),
			},
		},
	})
}

// TestAccAgentResource_Unauthorized401 proves a 401 from create surfaces
// the "mint a new token" guidance from the typed APIError.
func TestAccAgentResource_Unauthorized401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "token expired"})
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`mint a new token`),
			},
		},
	})
}

// TestAccAgentResource_BoundaryNotFound400 proves a 400
// ASSERTED_WORKLOAD_BOUNDARY_NOT_FOUND surfaces boundaryName in the error.
func TestAccAgentResource_BoundaryNotFound400(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      false,
			"error":        "functional boundary not found",
			"code":         "ASSERTED_WORKLOAD_BOUNDARY_NOT_FOUND",
			"boundaryName": "nonexistent-boundary",
		})
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(srv.URL) + `
resource "alterion_agent" "this" {
  environment            = "staging"
  cloud_provider         = "aws"
  cloud_account_id       = "123456789012"
  cloud_region           = "us-east-1"
  workload_name          = "ClaimsReview"
  functional_boundaries  = ["nonexistent-boundary"]
}
`,
				ExpectError: regexp.MustCompile(`nonexistent-boundary`),
			},
		},
	})
}

// TestAccAgentResource_Delete403WithoutAdopt proves a 403 on delete (agent
// owned by another principal, adopt not set) surfaces as a resource error.
func TestAccAgentResource_Delete403WithoutAdopt(t *testing.T) {
	createCalls := 0
	deleteCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agents/asserted":
			createCalls++
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "agentId": "asserted|staging|aws.123456789012.us-east-1.claims-review",
				"shortId": "shortid123", "status": "active", "isRegistered": true, "created": true,
			})
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "agentId": "asserted|staging|aws.123456789012.us-east-1.claims-review",
				"shortId": "shortid123", "displayName": "ClaimsReview", "status": "active", "isRegistered": true,
			})
		case r.Method == http.MethodDelete:
			deleteCalls++
			if deleteCalls == 1 {
				// First delete attempt (the one under test) 403s.
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "owned by another principal"})
				return
			}
			// Subsequent attempts (the test framework's own post-test
			// cleanup) succeed, so the test doesn't leave a dangling
			// resource behind on the fake server.
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "archivedAgentIds": []string{"asserted|staging|aws.123456789012.us-east-1.claims-review"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	config := providerConfig(srv.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{Config: config},
			{
				// Removing the resource from config plans/applies a
				// destroy; the fake server 403s it, which must surface as
				// an apply-time error rather than a silent success.
				Config:      providerConfig(srv.URL),
				ExpectError: regexp.MustCompile(`(?i)owned by another principal|Error Deleting Agent`),
			},
		},
	})
	if createCalls == 0 {
		t.Fatal("expected at least one create call")
	}
}

// TestAccAgentResource_AdoptTrue proves adopt = true lets delete succeed
// against an agent the fake server would otherwise 403 on.
func TestAccAgentResource_AdoptTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agents/asserted":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "agentId": "asserted|staging|aws.123456789012.us-east-1.claims-review",
				"shortId": "shortid123", "status": "active", "isRegistered": true, "created": true,
			})
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "agentId": "asserted|staging|aws.123456789012.us-east-1.claims-review",
				"shortId": "shortid123", "displayName": "ClaimsReview", "status": "active", "isRegistered": true,
			})
		case r.Method == http.MethodDelete:
			if r.URL.RawQuery != "adopt=true" {
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": false, "error": "owned by another principal"})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "archivedAgentIds": []string{"asserted|staging|aws.123456789012.us-east-1.claims-review"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	config := providerConfig(srv.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
  adopt            = true
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check:  resource.TestCheckResourceAttr("alterion_agent.this", "adopt", "true"),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_GatewayBaseURLComposedLocally proves
// gateway_base_url is computed from the provider's gateway_url when the
// server response's gatewayBaseUrl is null.
func TestAccAgentPathKeyDataSource_GatewayBaseURLComposedLocally(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"agentId":        "asserted|staging|aws.123456789012.us-east-1.claims-review",
			"shortId":        "92a778629f78",
			"pathPrefix":     "a",
			"gatewayBaseUrl": nil,
		})
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "alterion" {
  orion_url   = "` + srv.URL + `"
  api_token   = "orion_at_testtoken"
  gateway_url = "https://gw.example.com"
}

data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				Check: resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "gateway_base_url", "https://gw.example.com/a/92a778629f78"),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_GatewayBaseURLFromServer proves the
// server's own gatewayBaseUrl wins when present, even with gateway_url
// also configured.
func TestAccAgentPathKeyDataSource_GatewayBaseURLFromServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"agentId":        "asserted|staging|aws.123456789012.us-east-1.claims-review",
			"shortId":        "92a778629f78",
			"pathPrefix":     "a",
			"gatewayBaseUrl": "https://server-supplied.example.com/a/92a778629f78",
		})
	}))
	defer srv.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "alterion" {
  orion_url   = "` + srv.URL + `"
  api_token   = "orion_at_testtoken"
  gateway_url = "https://gw.example.com"
}

data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				Check: resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "gateway_base_url", "https://server-supplied.example.com/a/92a778629f78"),
			},
		},
	})
}

// TestAccProvider_MissingGatewayURL proves gateway_url is required: with
// neither the config attribute nor ALTERION_GATEWAY_URL set, Configure
// errors instead of silently leaving gateway_base_url uncomposed.
func TestAccProvider_MissingGatewayURL(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "alterion" {
  orion_url = "` + server.URL + `"
  api_token = "orion_at_testtoken"
}

data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Missing Gateway URL`),
			},
		},
	})
}

// TestAccProvider_GatewayURLFromEnv proves ALTERION_GATEWAY_URL satisfies
// the requirement when the config attribute is omitted, exactly like
// orion_url/api_token's own env fallbacks.
func TestAccProvider_GatewayURLFromEnv(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	t.Setenv("ALTERION_GATEWAY_URL", "https://gw-from-env.example.com")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: `
provider "alterion" {
  orion_url = "` + server.URL + `"
  api_token = "orion_at_testtoken"
}

data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				Check: resource.TestCheckResourceAttrSet("data.alterion_agent_path_key.this", "gateway_base_url"),
			},
		},
	})
}

// TestAccProvider_InvalidGatewayURL table-tests gateway_url rejections:
// bad scheme, no host, and a non-empty path (which would break the
// "<gateway_url>/<path_prefix>/<short_id>" composition).
func TestAccProvider_InvalidGatewayURL(t *testing.T) {
	cases := []struct {
		name       string
		gatewayURL string
	}{
		{name: "bad scheme", gatewayURL: "ftp://gw.example.com"},
		{name: "no host", gatewayURL: "https:///a"},
		{name: "has path", gatewayURL: "https://gw.example.com/gateway"},
		{name: "has query", gatewayURL: "https://gw.example.com?foo=bar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := newFakeOrionServer(t)
			defer server.Close()

			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
				Steps: []resource.TestStep{
					{
						Config: `
provider "alterion" {
  orion_url   = "` + server.URL + `"
  api_token   = "orion_at_testtoken"
  gateway_url = "` + tc.gatewayURL + `"
}

data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
						ExpectError: regexp.MustCompile(`Invalid Gateway URL`),
					},
				},
			})
		})
	}
}

// TestAccAgentResource_ProviderDefaultOverriddenByResource proves a
// resource-level identity attribute wins over the provider's default
// (precedence: resource/data-source > provider block).
func TestAccAgentResource_ProviderDefaultOverriddenByResource(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigWithCloudDefaults(server.URL, "gcp", "provider-default-project", "us-central1") + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_provider", "aws"),
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_account_id", "123456789012"),
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_region", "us-east-1"),
				),
			},
		},
	})
}
