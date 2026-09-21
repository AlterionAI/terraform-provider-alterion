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

// testAccProtoV6ProviderFactories are used to instantiate the provider
// during acceptance testing. These tests never talk to a real Orion
// deployment: they point orion_url at an in-process httptest server that
// fakes the REST contract, so no network access outside localhost occurs
// and no ALTERION_ORION_URL/ALTERION_API_TOKEN env vars are required.
func testAccProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"alterion": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// fakeAgentRecord is what the fake Orion server remembers about a
// registered agent, keyed by short id.
type fakeAgentRecord struct {
	agentID      string
	displayName  string
	status       string
	isRegistered bool
}

// fakeOrionServer stands in for the Orion web app's asserted-agent routes
// for acceptance-style tests, using the real server-side short-id
// derivation so responses are self-consistent. It keeps a small in-memory
// store so GET-by-short-id can 404 once an agent has been "archived" —
// including on demand via forget, for tests that simulate an agent going
// missing out from under Terraform.
type fakeOrionServer struct {
	*httptest.Server

	mu      sync.Mutex
	records map[string]*fakeAgentRecord
}

// forget removes an agent from the fake server's store, so a subsequent
// GET-by-short-id 404s as if the agent were deleted or archived directly
// against Orion, outside of Terraform.
func (f *fakeOrionServer) forget(shortID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.records, shortID)
}

// newFakeOrionServer starts a fakeOrionServer. Callers must defer
// server.Close().
func newFakeOrionServer(t *testing.T) *fakeOrionServer {
	t.Helper()

	fake := &fakeOrionServer{records: map[string]*fakeAgentRecord{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/agents/asserted/path-key", func(w http.ResponseWriter, r *http.Request) {
		environment := r.URL.Query().Get("environment")
		slug := r.URL.Query().Get("slug")
		shortID := client.ExpectedShortID(environment, slug)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":        true,
			"agentId":        "asserted|" + environment + "|" + slug,
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
		slug, _ := body["slug"].(string)
		displayName, _ := body["displayName"].(string)
		shortID := client.ExpectedShortID(environment, slug)
		agentID := "asserted|" + environment + "|" + slug

		fake.mu.Lock()
		fake.records[shortID] = &fakeAgentRecord{
			agentID:      agentID,
			displayName:  displayName,
			status:       "active",
			isRegistered: true,
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
				"archivedAgentIds": []string{"asserted|staging|claims-review"},
			})
		default:
			http.NotFound(w, r)
		}
	})

	fake.Server = httptest.NewServer(mux)
	return fake
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
  environment = "staging"
  slug        = "claims-review"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "environment", "staging"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "slug", "claims-review"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "agent_id", "asserted|staging|claims-review"),
					resource.TestCheckResourceAttr("data.alterion_agent_path_key.this", "short_id", "e42ec80aebee"),
					resource.TestCheckResourceAttrSet("data.alterion_agent_path_key.this", "gateway_base_url"),
				),
			},
		},
	})
}

func TestAccAgentPathKeyDataSource_InvalidSlug(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfig(server.URL) + `
data "alterion_agent_path_key" "this" {
  environment = "staging"
  slug        = "Not_Valid"
}
`,
				ExpectError: regexp.MustCompile(`Invalid Slug Format`),
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
  environment = "prod"
  slug        = "claims-review"
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
  environment  = "staging"
  slug         = "claims-review"
  display_name = "Claims Review Bot"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "environment", "staging"),
					resource.TestCheckResourceAttr("alterion_agent.this", "slug", "claims-review"),
					resource.TestCheckResourceAttr("alterion_agent.this", "agent_id", "asserted|staging|claims-review"),
					resource.TestCheckResourceAttr("alterion_agent.this", "short_id", "e42ec80aebee"),
					resource.TestCheckResourceAttr("alterion_agent.this", "status", "active"),
					resource.TestCheckResourceAttr("alterion_agent.this", "is_registered", "true"),
					resource.TestCheckResourceAttr("alterion_agent.this", "adopt", "false"),
				),
			},
			{
				// Update display_name in place (environment/slug unchanged).
				Config: providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment  = "staging"
  slug         = "claims-review"
  display_name = "Claims Review Bot v2"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "display_name", "Claims Review Bot v2"),
					resource.TestCheckResourceAttr("alterion_agent.this", "short_id", "e42ec80aebee"),
				),
			},
		},
	})
}

// TestAccAgentResource_RecreateOnMissing verifies that when the fake
// server reports the agent as gone (GET-by-short-id 404s, e.g. because it
// was archived directly against Orion outside of Terraform), the resource
// is dropped from state on refresh and the next plan shows it needs to be
// recreated, rather than silently going stale.
func TestAccAgentResource_RecreateOnMissing(t *testing.T) {
	server := newFakeOrionServer(t)
	defer server.Close()

	config := providerConfig(server.URL) + `
resource "alterion_agent" "this" {
  environment  = "staging"
  slug         = "claims-review"
  display_name = "Claims Review Bot"
}
`

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.this", "short_id", "e42ec80aebee"),
				),
			},
			{
				PreConfig:          func() { server.forget("e42ec80aebee") },
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
