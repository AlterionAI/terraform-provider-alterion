package client

import "testing"

// TestExpectedShortID pins the local short-id derivation to golden vectors
// taken from the Orion server implementation. If this test starts failing
// after an upstream change, the server's derivation changed and this
// helper (used only for a non-blocking diagnostic) needs to change with it.
//
// identityKey values are the server's internal "<cloudProvider>.
// <cloudAccountId>.<cloudRegion>.<workloadName>" composition (see
// IdentityKey) — never sent on the wire, only used to reproduce the hash
// locally.
func TestExpectedShortID(t *testing.T) {
	cases := []struct {
		environment string
		identityKey string
		want        string
	}{
		{"production", "aws.123456789012.us-east-1.support-bot", "5ca97feb80da"},
		{"staging", "aws.123456789012.us-west-2.claims-review", "325e91806aa1"},
		{"development", "gcp.my-gcp-project-1.us-central1.dev-agent", "06e296460bac"},
		{"production", "azure.11111111-1111-1111-1111-111111111111.eastus.a", "41851e90b5dd"},
		{"staging", "aws.123456789012.eu-west-2.agentcore-runtime-with-a-long-name-x", "1ecc6dc1d727"},
		// Case-sensitive vector: uppercase + underscore workload_name must
		// NOT be folded or lowercased — computed via IdentityKey below.
		{"production", IdentityKey(WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "My_Agent"}), "3b021fd0cf74"},
	}

	for _, tc := range cases {
		t.Run(tc.environment+"/"+tc.identityKey, func(t *testing.T) {
			got := ExpectedShortID(tc.environment, tc.identityKey)
			if got != tc.want {
				t.Errorf("ExpectedShortID(%q, %q) = %q, want %q", tc.environment, tc.identityKey, got, tc.want)
			}
		})
	}
}

// TestIdentityKey_CaseSensitive proves distinct workload names that differ
// only by case yield distinct identity keys — no lowercasing or folding
// anywhere in the composition.
func TestIdentityKey_CaseSensitive(t *testing.T) {
	upper := IdentityKey(WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "My_Agent"})
	lower := IdentityKey(WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "my_agent"})
	if upper == lower {
		t.Fatalf("expected case-sensitive identity keys to differ, both were %q", upper)
	}
	if upper != "aws.123456789012.us-east-1.My_Agent" {
		t.Errorf("unexpected identity key: %q", upper)
	}
	if lower != "aws.123456789012.us-east-1.my_agent" {
		t.Errorf("unexpected identity key: %q", lower)
	}
	if ExpectedShortID("production", upper) == ExpectedShortID("production", lower) {
		t.Fatalf("expected case-sensitive identity keys to hash to distinct short ids")
	}
}
