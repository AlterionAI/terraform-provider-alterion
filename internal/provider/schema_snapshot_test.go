package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// TestSchemaSnapshot marshals the provider/resource/data-source schemas via
// GetProviderSchema and compares them to testdata/schema.json. This is the
// customer-facing contract: any change here is a breaking or additive
// schema change a consumer would notice. Run with UPDATE_SNAPSHOT=1 to
// rewrite the golden file after an intentional schema change.
func TestSchemaSnapshot(t *testing.T) {
	server := providerserver.NewProtocol6(New("test")())()

	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}
	if len(resp.Diagnostics) > 0 {
		t.Fatalf("GetProviderSchema returned diagnostics: %+v", resp.Diagnostics)
	}

	got, err := json.MarshalIndent(struct {
		Provider          interface{}            `json:"provider"`
		ResourceSchemas   map[string]interface{} `json:"resource_schemas"`
		DataSourceSchemas map[string]interface{} `json:"data_source_schemas"`
	}{
		Provider:          resp.Provider,
		ResourceSchemas:   toInterfaceMap(resp.ResourceSchemas),
		DataSourceSchemas: toInterfaceMap(resp.DataSourceSchemas),
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	got = append(got, '\n')

	golden := filepath.Join("..", "..", "testdata", "schema.json")

	if os.Getenv("UPDATE_SNAPSHOT") == "1" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		t.Logf("wrote %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden file %s (run with UPDATE_SNAPSHOT=1 to create it): %v", golden, err)
	}

	if string(got) != string(want) {
		t.Errorf("schema drifted from %s; re-run with UPDATE_SNAPSHOT=1 if this schema change was intentional", golden)
	}
}

func toInterfaceMap[V any](m map[string]V) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
