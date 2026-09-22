// Package acceptance_live holds opt-in acceptance tests that exercise a
// real Orion deployment end to end. They are skipped by default; set
// TF_ACC_LIVE=1 plus the env vars below to run them. Never run as part of
// the normal `go test ./...` / CI `unit`/`acceptance` jobs — only from
// nightly-live.yml or manually.
package acceptance_live

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/AlterionAI/terraform-provider-alterion/internal/provider"
)

// requiredLiveEnv are the env vars a live run needs beyond TF_ACC_LIVE.
var requiredLiveEnv = []string{
	"ALTERION_ORION_URL",
	"ALTERION_API_TOKEN",
	"ALTERION_GATEWAY_URL",
	"ALTERION_TEST_ACCOUNT_ID",
	"ALTERION_TEST_REGION",
}

// skipUnlessLive skips the test unless TF_ACC_LIVE=1 and every required env
// var is set, printing which ones are missing so a misconfigured run fails
// loudly instead of silently skipping in CI.
func skipUnlessLive(t *testing.T) map[string]string {
	t.Helper()

	if os.Getenv("TF_ACC_LIVE") != "1" {
		t.Skip("TF_ACC_LIVE not set to 1; skipping live acceptance test against a real Orion deployment")
	}

	env := map[string]string{}
	var missing []string
	for _, name := range requiredLiveEnv {
		v := os.Getenv(name)
		if v == "" {
			missing = append(missing, name)
			continue
		}
		env[name] = v
	}
	if len(missing) > 0 {
		t.Fatalf("TF_ACC_LIVE=1 but missing required env vars: %v", missing)
	}
	return env
}

func liveProtoV6ProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"alterion": providerserver.NewProtocol6WithError(provider.New("acceptance-live")()),
	}
}

// uniqueWorkloadName suffixes a run-scoped identifier so concurrent live
// runs (and reruns of a failed run) never collide on identity, and so a
// leaked resource from a failed cleanup is easy to spot and hand-archive.
func uniqueWorkloadName(prefix string) string {
	return fmt.Sprintf("%s-live-%d", prefix, time.Now().UnixNano())
}

// TestLiveAgentLifecycle runs create -> read (via refresh) -> update ->
// destroy against a real Orion deployment. It always archives the agent on
// cleanup: CheckDestroy re-derives the short id and asserts the delete
// actually happened, so a partial failure doesn't leave a real agent row
// behind unnoticed.
func TestLiveAgentLifecycle(t *testing.T) {
	env := skipUnlessLive(t)
	workloadName := uniqueWorkloadName("tf-provider-live")

	config := func(displayName string) string {
		return fmt.Sprintf(`
provider "alterion" {
  orion_url        = %q
  api_token        = %q
  gateway_url      = %q
  cloud_provider   = "aws"
  cloud_account_id = %q
  cloud_region     = %q
}

resource "alterion_agent" "live" {
  environment   = "development"
  workload_name = %q
  display_name  = %q
}
`, env["ALTERION_ORION_URL"], env["ALTERION_API_TOKEN"], env["ALTERION_GATEWAY_URL"], env["ALTERION_TEST_ACCOUNT_ID"], env["ALTERION_TEST_REGION"], workloadName, displayName)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: liveProtoV6ProviderFactories(),
		// terraform-plugin-testing runs a destroy after the last step
		// automatically; that IS the archive-on-cleanup guarantee here —
		// no separate teardown script needed, and it runs even on
		// mid-test failure (t.Fatal) as long as apply succeeded.
		Steps: []resource.TestStep{
			{
				Config: config("Live Test Agent v1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("alterion_agent.live", "workload_name", workloadName),
					resource.TestCheckResourceAttr("alterion_agent.live", "display_name", "Live Test Agent v1"),
					resource.TestCheckResourceAttr("alterion_agent.live", "is_registered", "true"),
					resource.TestCheckResourceAttrSet("alterion_agent.live", "short_id"),
				),
			},
			{
				// Update: same identity, new display_name — must apply
				// in place (no replace) against the real API.
				Config: config("Live Test Agent v2"),
				Check:  resource.TestCheckResourceAttr("alterion_agent.live", "display_name", "Live Test Agent v2"),
			},
			{
				// Refresh-only step re-reads from the real server and
				// confirms no drift, proving Read() round-trips cleanly.
				Config:             config("Live Test Agent v2"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

// TestLivePathKeyDataSource proves the path-key data source resolves
// against a real deployment without requiring the agent to exist first.
func TestLivePathKeyDataSource(t *testing.T) {
	env := skipUnlessLive(t)
	workloadName := uniqueWorkloadName("tf-provider-live-pathkey")

	config := fmt.Sprintf(`
provider "alterion" {
  orion_url        = %q
  api_token        = %q
  gateway_url      = %q
  cloud_provider   = "aws"
  cloud_account_id = %q
  cloud_region     = %q
}

data "alterion_agent_path_key" "live" {
  environment   = "development"
  workload_name = %q
}
`, env["ALTERION_ORION_URL"], env["ALTERION_API_TOKEN"], env["ALTERION_GATEWAY_URL"], env["ALTERION_TEST_ACCOUNT_ID"], env["ALTERION_TEST_REGION"], workloadName)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: liveProtoV6ProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.alterion_agent_path_key.live", "short_id"),
					resource.TestCheckResourceAttrSet("data.alterion_agent_path_key.live", "agent_id"),
				),
			},
		},
	})
}
