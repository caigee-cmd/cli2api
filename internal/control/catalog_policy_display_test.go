package control

import (
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestProviderModelEntryListsDeclaredReasoningOptions(t *testing.T) {
	entry := ProviderModelEntry(providers.ModelInfo{
		PublicModel: "reasoning-model",
		Capabilities: providers.ModelCapabilities{
			Reasoning:        true,
			ReasoningOptions: []string{"low", "high"},
		},
	}, "qoder")
	values, ok := entry["reasoning_options"].([]string)
	if !ok {
		t.Fatalf("reasoning_options = %#v", entry["reasoning_options"])
	}
	want := "low,high"
	if got := strings.Join(values, ","); got != want {
		t.Fatalf("reasoning_options = %q, want %q", got, want)
	}
}

func TestModelCapabilitiesEntryOmitsUndeclaredReasoningOptions(t *testing.T) {
	entry := ModelCapabilitiesEntry(providers.ModelInfo{
		PublicModel: "reasoning-model",
		Capabilities: providers.ModelCapabilities{
			Reasoning: true,
		},
	})
	if _, ok := entry["reasoning_options"]; ok {
		t.Fatalf("reasoning model with no declared options must not invent any: %#v", entry["reasoning_options"])
	}
}
