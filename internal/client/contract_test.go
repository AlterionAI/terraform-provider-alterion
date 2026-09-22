package client

import "testing"

// TestExpectedShortID pins the local short-id derivation to golden vectors
// from the Orion server. A failure means the server's derivation changed
// and this diagnostic-only helper needs to change with it.
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
		// Case-sensitive: workload_name is never folded or lowercased.
		{"production", IdentityKey(WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "My_Agent"}), "3b021fd0cf74"},
		// One golden vector per cloud provider (spec vectors from the Orion server).
		{"staging", "gcp.my-project-123.us-central1.billing-agent", "18b99c7d9e63"},
		{"production", "azure.11111111-1111-1111-1111-111111111111.eastus.My_Container_App", "0ad91efc0253"},
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

// TestIdentityKey_CaseSensitive proves names differing only by case yield
// distinct identity keys.
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

// TestIdentityKey_Composition is table-driven across all three supported
// clouds: proves the join order/format is exactly
// "<cloudProvider>.<cloudAccountId>.<cloudRegion>.<workloadName>", verbatim,
// with no separator collisions or normalization.
func TestIdentityKey_Composition(t *testing.T) {
	cases := []struct {
		name     string
		identity WorkloadIdentity
		want     string
	}{
		{
			name:     "aws",
			identity: WorkloadIdentity{CloudProvider: "aws", CloudAccountID: "123456789012", CloudRegion: "us-east-1", WorkloadName: "support-bot"},
			want:     "aws.123456789012.us-east-1.support-bot",
		},
		{
			name:     "gcp",
			identity: WorkloadIdentity{CloudProvider: "gcp", CloudAccountID: "my-project-123", CloudRegion: "us-central1", WorkloadName: "billing-agent"},
			want:     "gcp.my-project-123.us-central1.billing-agent",
		},
		{
			name:     "azure",
			identity: WorkloadIdentity{CloudProvider: "azure", CloudAccountID: "11111111-1111-1111-1111-111111111111", CloudRegion: "eastus", WorkloadName: "My_Container_App"},
			want:     "azure.11111111-1111-1111-1111-111111111111.eastus.My_Container_App",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IdentityKey(tc.identity); got != tc.want {
				t.Errorf("IdentityKey(%+v) = %q, want %q", tc.identity, got, tc.want)
			}
		})
	}
}

// TestExpectedShortID_DistinctPerProvider proves that swapping only
// cloud_provider (identical account/region/name across clouds is
// unrealistic but the derivation must still not collide) yields distinct
// short ids — the provider segment is load-bearing in the hash input.
func TestExpectedShortID_DistinctPerProvider(t *testing.T) {
	seen := map[string]string{}
	for _, provider := range []string{"aws", "gcp", "azure"} {
		key := IdentityKey(WorkloadIdentity{CloudProvider: provider, CloudAccountID: "acct", CloudRegion: "region-1", WorkloadName: "agent"})
		shortID := ExpectedShortID("production", key)
		if other, ok := seen[shortID]; ok {
			t.Fatalf("short id %q collided between provider %q and %q", shortID, provider, other)
		}
		seen[shortID] = provider
	}
}
