package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/AlterionAI/terraform-provider-alterion/internal/client"
)

// testAccProtoV6ProviderFactories instantiates the provider for
// acceptance tests, which point orion_url at an in-process httptest
// server — no real Orion deployment, no env vars required.
func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"alterion": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// fakeAgentRecord is what the fake Orion server remembers, keyed by
// short id.
type fakeAgentRecord struct {
	agentID              string
	displayName          string
	status               string
	isRegistered         bool
	functionalBoundaries []string
}

// fakeOrionServer stands in for the Orion web app's asserted-agent
// routes, using the real short-id derivation so responses are
// self-consistent, with an in-memory store so GET-by-short-id can 404
// once an agent is "archived" (including on demand via forget).
type fakeOrionServer struct {
	*httptest.Server

	mu      sync.Mutex
	records map[string]*fakeAgentRecord
}

// forget makes a subsequent GET-by-short-id 404, simulating an agent
// deleted outside of Terraform.
func (f *fakeOrionServer) forget(shortID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, shortID)
}

// newFakeOrionServer starts a fakeOrionServer; callers must defer
// server.Close().
func newFakeOrionServer(t *testing.T) *fakeOrionServer {
	t.Helper()

	fake := &fakeOrionServer{records: map[string]*fakeAgentRecord{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/asserted/path-key", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		environment := q.Get("environment")
		identity := client.WorkloadIdentity{
			CloudProvider:  q.Get("cloudProvider"),
			CloudAccountID: q.Get("cloudAccountId"),
			CloudRegion:    q.Get("cloudRegion"),
			WorkloadName:   q.Get("workloadName"),
		}
		identityKey := client.IdentityKey(identity)
		shortID := client.ExpectedShortID(environment, identityKey)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"agentId":        "asserted|" + environment + "|" + identityKey,
			"shortId":        shortID,
			"pathPrefix":     "a",
			"gatewayBaseUrl": "https://gw.example.com/a/" + shortID,
		})
	})
	mux.HandleFunc("/api/v1/agents/asserted", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		environment, _ := body["environment"].(string)
		identity := client.WorkloadIdentity{
			CloudProvider:  stringField(body, "cloudProvider"),
			CloudAccountID: stringField(body, "cloudAccountId"),
			CloudRegion:    stringField(body, "cloudRegion"),
			WorkloadName:   stringField(body, "workloadName"),
		}
		displayName := stringField(body, "displayName")
		functionalBoundaries := stringSliceField(body, "functionalBoundaries")
		identityKey := client.IdentityKey(identity)
		shortID := client.ExpectedShortID(environment, identityKey)
		agentID := "asserted|" + environment + "|" + identityKey

		fake.mu.Lock()
		fake.records[shortID] = &fakeAgentRecord{
			agentID:              agentID,
			displayName:          displayName,
			status:               "active",
			isRegistered:         true,
			functionalBoundaries: functionalBoundaries,
		}
		fake.mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"agentId":      agentID,
			"shortId":      shortID,
			"status":       "active",
			"isRegistered": true,
			"created":      true,
		})
	})
	mux.HandleFunc("/api/v1/agents/asserted/", func(w http.ResponseWriter, r *http.Request) {
		shortID := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/asserted/")

		switch r.Method {
		case http.MethodGet:
			fake.mu.Lock()
			rec, ok := fake.records[shortID]
			fake.mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"success": false,
					"error":   "agent not found",
				})
				return
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success":      true,
				"agentId":      rec.agentID,
				"shortId":      shortID,
				"displayName":  rec.displayName,
				"status":       rec.status,
				"isRegistered": rec.isRegistered,
				"registeredBy": "orion_at_testtoken",
			})
		case http.MethodDelete:
			fake.mu.Lock()
			delete(fake.records, shortID)
			fake.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success":          true,
				"archivedAgentIds": []string{"asserted|staging|aws.123456789012.us-east-1.claims-review"},
			})
		default:
			http.NotFound(w, r)
		}
	})

	fake.Server = httptest.NewServer(mux)
	return fake
}

func stringField(body map[string]interface{}, key string) string {
	v, _ := body[key].(string)
	return v
}

func stringSliceField(body map[string]interface{}, key string) []string {
	raw, _ := body[key].([]interface{})
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TestAccAgentPathKeyDataSource_Basic(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "environment", "staging"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "cloud_provider", "aws"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "cloud_account_id", "123456789012"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "cloud_region", "us-east-1"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "workload_name", "ClaimsReview"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "agent_id", "asserted|staging|aws.123456789012.us-east-1.ClaimsReview"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "short_id", "92a778629f78"),
					resource.TestCheckResourceAttrSet("data.alterion_agent_path_key.this", "gateway_base_url"),
				),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_CloudProviderDefaultsToAWS verifies
// omitting cloud_provider defaults it to "aws".
func TestAccAgentPathKeyDataSource_CloudProviderDefaultsToAWS(t *testing.T) {
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
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "cloud_provider", "aws"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "short_id", "92a778629f78"),
				),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_CaseSensitive proves workload_name
// values differing only by case produce distinct agent/short ids.
func TestAccAgentPathKeyDataSource_CaseSensitive(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "upper" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "My_Agent"
}

data "alterion_agent_path_key" "lower" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "my_agent"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.upper", "agent_id", "asserted|staging|aws.123456789012.us-east-1.My_Agent"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.upper", "short_id", "834d67c34e7d"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.lower", "agent_id", "asserted|staging|aws.123456789012.us-east-1.my_agent"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.lower", "short_id", "2017186122f3"),
				),
			},
		},
	})
}

func TestAccAgentPathKeyDataSource_InvalidWorkloadName(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "not valid"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Workload Name`),
			},
		},
	})
}

func TestAccAgentPathKeyDataSource_InvalidCloudAccountID(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "not-an-account-id"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Cloud Account Id`),
			},
		},
	})
}

func TestAccAgentPathKeyDataSource_InvalidCloudRegion(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "US_EAST_1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Cloud Region`),
			},
		},
	})
}

func TestAccAgentPathKeyDataSource_InvalidCloudProvider(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "staging"
  cloud_provider   = "oracle-cloud"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Cloud Provider`),
			},
		},
	})
}

func TestAccAgentPathKeyDataSource_InvalidEnvironment(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment      = "prod"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Environment`),
			},
		},
	})
}

func TestAccAgentResource_Lifecycle(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
  display_name     = "Claims Review Bot"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "environment", "staging"),
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_provider", "aws"),
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_account_id", "123456789012"),
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_region", "us-east-1"),
					resource.TestCheckResourceAttr("alterion_agent.this", "workload_name", "ClaimsReview"),
					resource.TestCheckResourceAttr("alterion_agent.this", "agent_id", "asserted|staging|aws.123456789012.us-east-1.ClaimsReview"),
					resource.TestCheckResourceAttr("alterion_agent.this", "short_id", "92a778629f78"),
					resource.TestCheckResourceAttr("alterion_agent.this", "status", "active"),
					resource.TestCheckResourceAttr("alterion_agent.this", "is_registered", "true"),
					resource.TestCheckResourceAttr("alterion_agent.this", "adopt", "false"),
				),
			},
			{
				Config: providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
  display_name     = "Claims Review Bot v2"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "display_name", "Claims Review Bot v2"),
					resource.TestCheckResourceAttr("alterion_agent.this", "short_id", "92a778629f78"),
				),
			},
		},
	})
}

// TestAccAgentResource_DisplayNameDefaultsToWorkloadName verifies
// omitting display_name defaults it to workload_name.
func TestAccAgentResource_DisplayNameDefaultsToWorkloadName(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "display_name", "ClaimsReview"),
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_provider", "aws"),
				),
			},
		},
	})
}

// TestAccAgentResource_WithWorkloadResourceIDAndType covers the optional
// workload_resource_id / workload_type attributes with a non-AWS shape.
func TestAccAgentResource_WithWorkloadResourceIDAndType(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment           = "staging"
  cloud_provider        = "gcp"
  cloud_account_id      = "my-gcp-project-1"
  cloud_region          = "us-central1"
  workload_name         = "billing-agent"
  workload_resource_id  = "projects/my-gcp-project-1/locations/us-central1/services/billing-agent"
  workload_type         = "cloud-run-service"
  display_name          = "Billing Agent"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "cloud_provider", "gcp"),
					resource.TestCheckResourceAttr("alterion_agent.this", "workload_resource_id", "projects/my-gcp-project-1/locations/us-central1/services/billing-agent"),
					resource.TestCheckResourceAttr("alterion_agent.this", "workload_type", "cloud-run-service"),
				),
			},
		},
	})
}

// TestAccAgentResource_RecreateOnMissing verifies that when the fake
// server 404s the agent, refresh drops it from state and the next plan
// shows it needs recreating.
func TestAccAgentResource_RecreateOnMissing(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	config := providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
  display_name     = "Claims Review Bot"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "short_id", "92a778629f78"),
				),
			},
			{
				PreConfig:          func() { server.forget("92a778629f78") },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

func providerConfig(baseURL string) string {
	return `
provider "alterion" {
  orion_url  = "` + baseURL + `"
  api_token  = "orion_at_testtoken"
}
`
}

func providerConfigWithCloudDefaults(baseURL, cloudProvider, cloudAccountID, cloudRegion string) string {
	return `
provider "alterion" {
  orion_url        = "` + baseURL + `"
  api_token        = "orion_at_testtoken"
  cloud_provider   = "` + cloudProvider + `"
  cloud_account_id = "` + cloudAccountID + `"
  cloud_region     = "` + cloudRegion + `"
}
`
}

// TestAccAgentPathKeyDataSource_ProviderCloudDefaults verifies omitting
// cloud_provider/cloud_account_id/cloud_region falls back to the
// provider block.
func TestAccAgentPathKeyDataSource_ProviderCloudDefaults(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigWithCloudDefaults(server.URL, "gcp", "my-gcp-project-1", "us-central1") + `
data "alterion_agent_path_key" "this" {
  environment   = "staging"
  workload_name = "ClaimsReview"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "cloud_provider", "gcp"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "cloud_account_id", "my-gcp-project-1"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "cloud_region", "us-central1"),
				),
			},
		},
	})
}

// TestAccAgentPathKeyDataSource_MissingCloudAccountID verifies omitting
// cloud_account_id at both levels errors.
func TestAccAgentPathKeyDataSource_MissingCloudAccountID(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment  = "staging"
  cloud_region = "us-east-1"
  workload_name = "ClaimsReview"
}
`,
				ExpectError: regexp.MustCompile(`Missing Cloud Account Id`),
			},
		},
	})
}

// TestAccAgentResource_ProviderCloudDefaults verifies the resource also
// falls back to the provider's cloud defaults.
func TestAccAgentResource_ProviderCloudDefaults(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigWithCloudDefaults(server.URL, "aws", "123456789012", "us-east-1") + `
resource "alterion_agent" "this" {
  environment   = "staging"
  workload_name = "ClaimsReview"
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

// TestAccAgentResource_FunctionalBoundaries verifies the attribute
// round-trips through plan/apply.
func TestAccAgentResource_FunctionalBoundaries(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment      = "staging"
  cloud_provider   = "aws"
  cloud_account_id = "123456789012"
  cloud_region     = "us-east-1"
  workload_name    = "ClaimsReview"
  functional_boundaries = ["production-support", "finance"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "functional_boundaries.#", "2"),
					resource.TestCheckResourceAttr("alterion_agent.this", "functional_boundaries.0", "production-support"),
					resource.TestCheckResourceAttr("alterion_agent.this", "functional_boundaries.1", "finance"),
				),
			},
		},
	})
}
