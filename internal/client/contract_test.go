package client

import "testing"

// TestExpectedShortID pins the local short-id derivation to golden vectors
// taken from the Orion server implementation. If this test starts failing
// after an upstream change, the server's derivation changed and this
// helper (used only for a non-blocking diagnostic) needs to change with it.
func TestExpectedShortID(t *testing.T) {
	cases := []struct {
		environment string
		slug        string
		want        string
	}{
		{"production", "123456789012-us-east-1-support-bot", "75a30ffb2baa"},
		{"staging", "claims-review", "e42ec80aebee"},
		{"development", "dev", "20f71dfe77eb"},
		{"production", "a", "6d34bfea8291"},
		{"staging", "123456789012-eu-west-2-agentcore-runtime-with-a-long-name-x", "e0491dfce420"},
	}

	for _, tc := range cases {
		t.Run(tc.environment+"/"+tc.slug, func(t *testing.T) {
			got := ExpectedShortID(tc.environment, tc.slug)
			if got != tc.want {
				t.Errorf("ExpectedShortID(%q, %q) = %q, want %q", tc.environment, tc.slug, got, tc.want)
			}
		})
	}
}
