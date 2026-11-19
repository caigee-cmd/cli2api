package control

import (
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestProviderModelEntryListsFullGatewayReasoningOptions(t *testing.T) {
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
	want := "low,high,none,medium,xhigh,max"
	if got := strings.Join(values, ","); got != want {
		t.Fatalf("reasoning_options = %q, want %q", got, want)
	}
}

func TestModelCapabilitiesEntryListsFullGatewayReasoningOptions(t *testing.T) {
	entry := ModelCapabilitiesEntry(providers.ModelInfo{
		PublicModel: "reasoning-model",
		Capabilities: providers.ModelCapabilities{
			Reasoning: true,
		},
	})
	values, ok := entry["reasoning_options"].([]string)
	if !ok {
		t.Fatalf("reasoning_options = %#v", entry["reasoning_options"])
	}
	want := "none,low,medium,high,xhigh,max"
	if got := strings.Join(values, ","); got != want {
		t.Fatalf("reasoning_options = %q, want %q", got, want)
	}
}
