package orcarouter

import (
	"context"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// fixtureCatalog covers every capability the filters must distinguish:
// text-only chat, image-input chat, embedding, image generation, video, rerank,
// and a record this build cannot speak at all.
const fixtureCatalog = `{
  "object": "list",
  "data": [
    {"id":"deepseek/deepseek-v4-pro","object":"model","owned_by":"DeepSeek",
     "context_length":1048576,"max_completion_tokens":384000,
     "supported_endpoint_types":["openai","openai-response"],
     "architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
    {"id":"deepseek/deepseek-v4.1-flash","object":"model","owned_by":"DeepSeek",
     "context_length":1048576,"max_completion_tokens":384000,
     "supported_endpoint_types":["openai","anthropic","openai-response"],
     "architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
    {"id":"orcarouter/auto","object":"model","owned_by":"orcarouter",
     "supported_endpoint_types":["openai","openai-response","anthropic","gemini"],
     "architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
    {"id":"vendor/embed-1","object":"model","owned_by":"Vendor",
     "supported_endpoint_types":["embedding"],
     "architecture":{"input_modalities":["text"],"output_modalities":["embedding"]}},
    {"id":"vendor/image-gen-1","object":"model","owned_by":"Vendor",
     "supported_endpoint_types":["image-generation"],
     "architecture":{"input_modalities":["text"],"output_modalities":["image"]}},
    {"id":"vendor/video-1","object":"model","owned_by":"Vendor",
     "supported_endpoint_types":["openai-video"],
     "architecture":{"input_modalities":["text"],"output_modalities":["video"]}},
    {"id":"vendor/rerank-1","object":"model","owned_by":"Vendor",
     "supported_endpoint_types":["jina-rerank"],
     "architecture":{"input_modalities":["text"],"output_modalities":["score"]}},
    {"id":"vendor/unspeakable-1","object":"model","owned_by":"Vendor",
     "supported_endpoint_types":["some-future-route"],
     "architecture":{"input_modalities":["text"],"output_modalities":["text"]}}
  ]
}`

func parseFixture(t *testing.T) []providers.ModelInfo {
	t.Helper()
	models, err := ParseCatalogJSON([]byte(fixtureCatalog))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return models
}

func ids(models []providers.ModelInfo) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.PublicModel)
	}
	return out
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestParseCatalogKeepsNamespaceAndDropsUnspeakableRecords(t *testing.T) {
	models := parseFixture(t)
	got := ids(models)
	if contains(got, "vendor/unspeakable-1") {
		t.Fatal("a record advertising only routes this build cannot speak must be dropped")
	}
	if !contains(got, "deepseek/deepseek-v4-pro") {
		t.Fatal("vendor/model namespace must be preserved verbatim")
	}
	if !contains(got, "vendor/embed-1") || !contains(got, "vendor/image-gen-1") {
		t.Fatal("non-chat capability records must be retained so their capability filters can find them")
	}
}

func TestChatFilterRequiresSpeakableEndpointAndExcludesNonText(t *testing.T) {
	models := parseFixture(t)
	chat := ids(FilterModels(models, CapabilityChat))
	for _, want := range []string{"deepseek/deepseek-v4-pro", "deepseek/deepseek-v4.1-flash", "orcarouter/auto"} {
		if !contains(chat, want) {
			t.Fatalf("chat filter dropped %q: %v", want, chat)
		}
	}
	for _, unwanted := range []string{"vendor/embed-1", "vendor/image-gen-1", "vendor/video-1", "vendor/rerank-1"} {
		if contains(chat, unwanted) {
			t.Fatalf("chat filter must exclude %q: %v", unwanted, chat)
		}
	}
}

// Multimodal fails closed: only a model whose catalog metadata declares image
// input may appear in the image-capable chat list.
func TestMultimodalFilterFailsClosed(t *testing.T) {
	models := parseFixture(t)
	vision := ids(FilterModels(models, CapabilityVision))
	if contains(vision, "deepseek/deepseek-v4-pro") {
		t.Fatalf("a text-only model must not appear once an image is attached: %v", vision)
	}
	if !contains(vision, "deepseek/deepseek-v4.1-flash") {
		t.Fatalf("the image-input model must remain: %v", vision)
	}
	if contains(vision, "vendor/image-gen-1") {
		t.Fatal("an image *generation* model is not a chat model with image input")
	}
	// A model with no architecture block at all must fail closed.
	bare := []providers.ModelInfo{{NativeModel: "x/y", PublicModel: "x/y",
		Capabilities: providers.ModelCapabilities{EndpointTypes: []string{"openai"}}}}
	if got := FilterModels(bare, CapabilityVision); len(got) != 0 {
		t.Fatalf("undeclared image modality must fail closed, got %v", ids(got))
	}
}

func TestEmbeddingImageVideoRerankFilters(t *testing.T) {
	models := parseFixture(t)
	if got := ids(FilterModels(models, CapabilityEmbedding)); len(got) != 1 || got[0] != "vendor/embed-1" {
		t.Fatalf("embedding filter: %v", got)
	}
	if got := ids(FilterModels(models, CapabilityImage)); len(got) != 1 || got[0] != "vendor/image-gen-1" {
		t.Fatalf("image-generation filter: %v", got)
	}
	if got := ids(FilterModels(models, CapabilityVideoGeneration)); len(got) != 1 || got[0] != "vendor/video-1" {
		t.Fatalf("video-generation filter: %v", got)
	}
	if got := ids(FilterModels(models, CapabilityRerank)); len(got) != 1 || got[0] != "vendor/rerank-1" {
		t.Fatalf("rerank filter: %v", got)
	}
}

// A model that gains or loses image input must move between selectors, and a
// stale selection must be detectable as no longer compatible.
func TestProviderCapabilityChangeRecomputesSelection(t *testing.T) {
	textOnly := providers.ModelInfo{PublicModel: "v/m", NativeModel: "v/m",
		Capabilities: providers.ModelCapabilities{EndpointTypes: []string{"openai"}, InputModalities: []string{"text"}}}
	if !MatchesCapability(textOnly, CapabilityChat) {
		t.Fatal("text-only model must be a chat model")
	}
	if MatchesCapability(textOnly, CapabilityVision) {
		t.Fatal("text-only model must not be selectable for vision")
	}
	withImage := textOnly
	withImage.Capabilities.ImageInput = true
	withImage.Capabilities.InputModalities = []string{"text", "image"}
	if !MatchesCapability(withImage, CapabilityVision) {
		t.Fatal("a model declaring image input must become selectable for vision")
	}
	// The previously selected text model is no longer in the vision options.
	options := FilterModels([]providers.ModelInfo{textOnly}, CapabilityVision)
	if len(options) != 0 {
		t.Fatal("a stale text selection must be cleared when the requirement changes")
	}
}

func TestCatalogDiscoveryUsesLiveAPI(t *testing.T) {
	hits := &hitCounter{paths: map[string]int{}}
	srv := newJSONServer(t, hits, func(string) (int, string) { return 200, fixtureCatalog })
	store := newFakeStore()
	payload, _ := Credential{Format: CredentialFormat, APIKey: "sk-orca-LIVE0000000000000", APIBase: srv.URL}.Encode()
	store.format, store.payload = CredentialFormat, payload
	client := NewClient(store)

	models, err := client.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	if !contains(ids(models), "deepseek/deepseek-v4-pro") {
		t.Fatalf("live catalog must be surfaced: %v", ids(models))
	}
	if hits.snapshot()[ModelsPath] != 1 {
		t.Fatalf("expected one catalog call, got %v", hits.snapshot())
	}
	// A warm cache must not re-hit the API.
	if _, err := client.Models(context.Background(), "acc-1"); err != nil {
		t.Fatalf("cached models: %v", err)
	}
	if hits.snapshot()[ModelsPath] != 1 {
		t.Fatalf("a warm cache must not refetch, got %v", hits.snapshot())
	}
}

// When live discovery fails and nothing is cached, only the clearly-labelled,
// verified seed is offered — never free text and never an unverified example.
func TestLiveDiscoveryFailureFallsBackToVerifiedSeed(t *testing.T) {
	hits := &hitCounter{paths: map[string]int{}}
	srv := newJSONServer(t, hits, func(string) (int, string) {
		return 502, `{"error":{"message":"catalog unavailable"}}`
	})
	store := newFakeStore()
	payload, _ := Credential{Format: CredentialFormat, APIKey: "sk-orca-SEED0000000000000", APIBase: srv.URL}.Encode()
	store.format, store.payload = CredentialFormat, payload
	client := NewClient(store)

	models, err := client.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("a catalog outage must not be fatal: %v", err)
	}
	got := ids(models)
	for _, want := range []string{"openai/gpt-5.5", "anthropic/claude-opus-4.8", "google/gemini-3.5-flash", "deepseek/deepseek-v4-pro", "orcarouter/auto"} {
		if !contains(got, want) {
			t.Fatalf("verified seed missing %q: %v", want, got)
		}
	}
	// The seed must keep its verified reasoning ladder and modalities.
	for _, model := range models {
		if model.PublicModel == "openai/gpt-5.5" {
			if len(model.Capabilities.ReasoningOptions) == 0 {
				t.Fatal("the seed must retain the verified reasoning ladder")
			}
			for _, level := range []string{"low", "medium", "high", "xhigh"} {
				if !contains(model.Capabilities.ReasoningOptions, level) {
					t.Fatalf("gpt-5.5 seed lost the %q effort level", level)
				}
			}
		}
		if model.PublicModel == "deepseek/deepseek-v4-pro" && model.Capabilities.ImageInput {
			t.Fatal("a text-only seed model must not claim image input")
		}
	}
	// The seed must stay inside the chat filter.
	if len(FilterModels(models, CapabilityChat)) != len(models) {
		t.Fatal("every verified seed entry must be a usable text chat model")
	}
	// No unverifiable example model may sneak in.
	if contains(got, "openai/gpt-5") || contains(got, "openai/gpt-4o") {
		t.Fatalf("seed must not contain unverified example ids: %v", got)
	}
}

// A successful live run is authoritative: the seed is never merged into it.
func TestLiveCatalogIsAuthoritativeWithoutSeedMixedIn(t *testing.T) {
	srv := newJSONServer(t, &hitCounter{paths: map[string]int{}}, func(string) (int, string) { return 200, fixtureCatalog })
	store := newFakeStore()
	payload, _ := Credential{Format: CredentialFormat, APIKey: "sk-orca-AUTH0000000000000", APIBase: srv.URL}.Encode()
	store.format, store.payload = CredentialFormat, payload
	client := NewClient(store)

	models, err := client.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	for _, model := range models {
		if strings.HasPrefix(model.PublicModel, "openai/gpt-5.5") {
			t.Fatal("a successful live catalog must not be polluted with seed entries")
		}
	}
}

func TestCatalogBoundRejectsOversizedItemCount(t *testing.T) {
	var builder strings.Builder
	builder.WriteString(`{"object":"list","data":[`)
	for i := 0; i < maxCatalogItems+1; i++ {
		if i > 0 {
			builder.WriteString(",")
		}
		builder.WriteString(`{"id":"v/m","supported_endpoint_types":["openai"]}`)
	}
	builder.WriteString(`]}`)
	if _, err := ParseCatalogJSON([]byte(builder.String())); err == nil {
		t.Fatal("an over-large catalog must be refused")
	}
}

func TestCatalogHandlesEmptyAndMalformedResponses(t *testing.T) {
	if _, err := ParseCatalogJSON([]byte(`{"object":"list","data":[]}`)); err == nil {
		t.Fatal("an empty catalog must be refused rather than yielding an empty selector")
	}
	if _, err := ParseCatalogJSON([]byte(`not json`)); err == nil {
		t.Fatal("a malformed catalog must be refused")
	}
	// A model with no usable endpoint type is not an error on its own, but a
	// catalog with nothing usable is.
	if _, err := ParseCatalogJSON([]byte(`{"data":[{"id":"v/m","supported_endpoint_types":["mystery"]}]}`)); err == nil {
		t.Fatal("a catalog with no speakable model must be refused")
	}
}
