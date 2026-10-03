package control

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func capEntry(name string, caps providers.ModelCapabilities) map[string]any {
	model := providers.ModelInfo{NativeModel: name, PublicModel: name, Capabilities: caps}
	return ProviderModelEntry(model, "orcarouter")
}

// The catalog is the only source of truth for a model selector. Every entry a
// filter lets through must have declared the capability in its metadata, and a
// capability the upstream never declared must not leak a model into the list.
func TestFilterModelsByCapabilityUsesDeclaredMetadata(t *testing.T) {
	entries := []map[string]any{
		capEntry("vendor/text-only", providers.ModelCapabilities{
			EndpointTypes: []string{"openai"}, InputModalities: []string{"text"},
		}),
		capEntry("vendor/vision", providers.ModelCapabilities{
			EndpointTypes: []string{"openai"}, InputModalities: []string{"text", "image"}, ImageInput: true,
		}),
		capEntry("vendor/embed", providers.ModelCapabilities{
			EndpointTypes: []string{"embeddings"}, InputModalities: []string{"text"}, Embedding: true,
		}),
		capEntry("vendor/draw", providers.ModelCapabilities{
			EndpointTypes: []string{"image-generation"}, OutputModalities: []string{"image"}, ImageGeneration: true,
		}),
		capEntry("vendor/movie", providers.ModelCapabilities{
			EndpointTypes: []string{"openai-video"}, VideoInput: true,
		}),
		capEntry("vendor/rank", providers.ModelCapabilities{
			EndpointTypes: []string{"jina-rerank"}, Rerank: true,
		}),
	}

	cases := []struct {
		capability string
		want       []string
	}{
		{"", []string{"vendor/text-only", "vendor/vision", "vendor/embed", "vendor/draw", "vendor/movie", "vendor/rank"}},
		{"chat", []string{"vendor/text-only", "vendor/vision", "vendor/embed", "vendor/draw", "vendor/movie", "vendor/rank"}},
		{"vision", []string{"vendor/vision"}},
		{"embedding", []string{"vendor/embed"}},
		{"image", []string{"vendor/draw"}},
		{"video", []string{"vendor/movie"}},
		{"rerank", []string{"vendor/rank"}},
		// Unknown capabilities fail closed: guessing from a model name is
		// forbidden, so the selector must show nothing rather than everything.
		{"audio", nil},
		{"telepathy", nil},
	}
	for _, tc := range cases {
		t.Run(tc.capability, func(t *testing.T) {
			got := FilterModelsByCapability(entries, tc.capability)
			if len(got) != len(tc.want) {
				t.Fatalf("capability %q: got %d models, want %d", tc.capability, len(got), len(tc.want))
			}
			for i, want := range tc.want {
				if got[i]["id"] != want {
					t.Fatalf("capability %q: model %d = %v, want %s", tc.capability, i, got[i]["id"], want)
				}
			}
		})
	}
}

// An entry whose catalog metadata omits a modality must never be advertised as
// accepting it, even when the model name looks multimodal.
func TestFilterModelsByCapabilityFailsClosedOnUndeclaredModality(t *testing.T) {
	entries := []map[string]any{
		capEntry("named-like-a-vision-model", providers.ModelCapabilities{
			EndpointTypes: []string{"openai"}, InputModalities: []string{"text"},
		}),
	}
	if got := FilterModelsByCapability(entries, "vision"); len(got) != 0 {
		t.Fatalf("undeclared image input leaked into the vision selector: %v", got)
	}
}

// The transport metadata that the OrcaRouter catalog supplies has to survive
// into the console entry, because the selector filters on those exact fields.
func TestProviderModelEntryCarriesCapabilityMetadata(t *testing.T) {
	entry := capEntry("anthropic/claude-opus-4.8", providers.ModelCapabilities{
		EndpointTypes:   []string{"anthropic", "openai"},
		InputModalities: []string{"text", "image"},
		ImageInput:      true,
		ImageGeneration: false,
	})
	for _, field := range []string{"input_modalities", "endpoint_types"} {
		if _, ok := entry[field]; !ok {
			t.Fatalf("entry is missing %s: %v", field, entry)
		}
	}
	if entry["image_input"] != true {
		t.Fatalf("declared image input was dropped: %v", entry)
	}
	if _, ok := entry["image_generation"]; ok {
		t.Fatalf("undeclared capability must stay absent: %v", entry)
	}
	// Text-only models must not be silently upgraded by the merge helper.
	MergeModelEntryCapabilities(entry, capEntry("anthropic/claude-opus-4.8", providers.ModelCapabilities{}))
	if entry["image_input"] != true {
		t.Fatalf("merge dropped a still-declared capability: %v", entry)
	}
}
