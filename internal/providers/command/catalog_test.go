package command

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestParseCatalogFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/command_models.sample.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	models, err := ParseCatalogJSON(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected models")
	}
	byID := map[string]providers.ModelInfo{}
	for _, m := range models {
		if m.NativeModel != m.PublicModel {
			t.Fatalf("native/public mismatch for %q: %q", m.NativeModel, m.PublicModel)
		}
		byID[m.NativeModel] = m
	}
	deepseek, ok := byID["deepseek/deepseek-v4-pro"]
	if !ok {
		t.Fatal("deepseek/deepseek-v4-pro missing")
	}
	if !deepseek.Capabilities.Tools {
		t.Error("models must advertise tool support")
	}
	if deepseek.Capabilities.ContextWindow != 1000000 {
		t.Errorf("context window = %d, want 1000000", deepseek.Capabilities.ContextWindow)
	}
	if deepseek.DisplayName == "" {
		t.Error("display name should fall back to the id, not be empty")
	}
	if deepseek.Capabilities.Reasoning != true {
		t.Error("deepseek/deepseek-v4-pro must advertise the verified reasoning levels")
	}
	if got := strings.Join(deepseek.Capabilities.ReasoningOptions, ","); got != "high,max" {
		t.Errorf("deepseek reasoning options = %q, want high,max", got)
	}
	if len(deepseek.Capabilities.ReasoningOptions) > 0 {
		// A model without verified reasoning metadata must not advertise any.
		if sonnet, ok := byID["claude-sonnet-5"]; ok && sonnet.Capabilities.Reasoning {
			t.Error("models without a declared reasoning map must not claim reasoning")
		}
	}
	// Slash-bearing ids survive intact.
	if _, ok := byID["moonshotai/Kimi-K3"]; !ok {
		t.Error("slashed id moonshotai/Kimi-K3 missing")
	}
}

func TestParseCatalogEmpty(t *testing.T) {
	if _, err := ParseCatalogJSON([]byte(`{"object":"list","data":[]}`)); err == nil {
		t.Fatal("empty catalog must error")
	}
}

func TestModelsSendsAuthAndCaches(t *testing.T) {
	srv, rec := catalogServer(t)
	client, _ := newTestClient(t, srv)

	models, err := client.Models(t.Context(), "acc-1")
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("expected models from server")
	}
	if got := rec.header("Authorization"); got != "Bearer user_test_key_1234567890" {
		t.Errorf("auth header = %q", got)
	}
	if rec.count(PathModels) != 1 {
		t.Fatalf("catalog hits = %d, want 1", rec.count(PathModels))
	}

	// A second call must be served from the TTL cache (no second upstream hit).
	if _, err := client.Models(t.Context(), "acc-1"); err != nil {
		t.Fatalf("Models cached: %v", err)
	}
	if rec.count(PathModels) != 1 {
		t.Errorf("catalog not cached: hits = %d, want 1", rec.count(PathModels))
	}
}

func TestFixtureShape(t *testing.T) {
	raw, err := os.ReadFile("testdata/command_models.sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Object string `json:"object"`
		Data   []struct {
			ID                 string   `json:"id"`
			SupportedEndpoints []string `json:"supported_endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	if envelope.Object != "list" {
		t.Errorf("fixture object = %q, want list", envelope.Object)
	}
	if len(envelope.Data) == 0 {
		t.Fatal("fixture has no models")
	}
}
