package orcarouter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// upstream is a local stand-in for api.orcarouter.ai/v1 used by the seam tests.
// It replies to /models and /chat/completions and records the bearer key it saw,
// so a test can prove the downstream path does not care which adapter supplied
// the credential.
type upstream struct {
	modelsBody string
	seenKeys   []string
	seenPaths  []string
}

func newUpstream(t *testing.T) (*httptest.Server, *upstream) {
	t.Helper()
	up := &upstream{modelsBody: liveModelsBody}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		up.seenKeys = append(up.seenKeys, r.Header.Get("Authorization"))
		up.seenPaths = append(up.seenPaths, r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, up.modelsBody)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		up.seenKeys = append(up.seenKeys, r.Header.Get("Authorization"))
		up.seenPaths = append(up.seenPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"c1","object":"chat.completion","model":"deepseek/deepseek-v4-pro","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, up
}

// liveModelsBody mirrors the verified live catalog wire shape, including the
// capability fields the selectors filter on.
const liveModelsBody = `{"object":"list","data":[
 {"id":"deepseek/deepseek-v4-pro","object":"model","owned_by":"deepseek","context_length":1048576,"max_completion_tokens":384000,
  "supported_endpoint_types":["openai","openai-response"],"architecture":{"input_modalities":["text"],"output_modalities":["text"]},
  "name":"DeepSeek V4 Pro","pricing":{"prompt":"0.000001"}},
 {"id":"anthropic/claude-opus-4.8","object":"model","owned_by":"anthropic","context_length":200000,"max_completion_tokens":64000,
  "supported_endpoint_types":["anthropic","openai"],"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},
  "name":"Claude Opus 4.8"},
 {"id":"vendor/embed-x","object":"model","owned_by":"vendor","context_length":8000,
  "supported_endpoint_types":["embeddings"],"architecture":{"input_modalities":["text"],"output_modalities":["embedding"]},
  "name":"Embed X"},
 {"id":"vendor/draw-x","object":"model","owned_by":"vendor",
  "supported_endpoint_types":["image-generation"],"architecture":{"input_modalities":["text"],"output_modalities":["image"]},
  "name":"Draw X"}
]}`

func upstreamStore(t *testing.T, srv *httptest.Server) *fakeStore {
	t.Helper()
	return upstreamStoreFor(t, srv, "sk-orca-UPSTREAM0000000000")
}

func upstreamStoreFor(t *testing.T, srv *httptest.Server, apiKey string) *fakeStore {
	t.Helper()
	store := newFakeStore()
	store.secrets[SecretAPIBase] = srv.URL + "/v1"
	store.secrets[SecretAuthBase] = srv.URL
	payload, err := Credential{Format: CredentialFormat, APIKey: apiKey, APIBase: srv.URL + "/v1"}.Encode()
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	store.format, store.payload = CredentialFormat, payload
	return store
}

// The API-key adapter and the PKCE adapter must both drive the very same
// downstream chat + model-discovery path: the provider asks the credential seam
// for a bearer key and never branches on which adapter produced it.
func TestBothAuthAdaptersDriveSameDownstreamPath(t *testing.T) {
	oauthSrv, fakeAuth := newFakeAuthServer(t)
	srv, up := newUpstream(t)

	// Both adapters must expose the same credential seam type; only the id and
	// the presence of a login session differ.
	apiStore := upstreamStore(t, srv)
	apiClient := NewClient(apiStore)
	apiAdapter := apiClient.Adapter()
	if apiAdapter.ID != ProviderID || apiAdapter.Login != nil {
		t.Fatal("the API-key entry must not start a login")
	}
	if _, ok := apiAdapter.Credential.(providers.CredentialImporter); !ok {
		t.Fatal("the API-key entry must support credential import validation")
	}

	// Entry 1: paste an sk-orca-… key through the API-key entry's codec. The
	// credential keeps the inference origin reachable at the local test server.
	inner, _ := Credential{Format: CredentialFormat, APIKey: "sk-orca-PASTED0000000000", APIBase: srv.URL + "/v1"}.Encode()
	if err := apiAdapter.Credential.Validate(inner); err != nil {
		t.Fatalf("api-key credential rejected: %v", err)
	}
	if err := apiStore.SaveCredentialPayload(context.Background(), "acc-1", CredentialFormat, inner); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Entry 2: run the real PKCE flow through this account's connect adapter.
	oauthStore := newFakeStore()
	oauthStore.secrets[SecretAuthBase] = oauthSrv.URL
	oauthStore.secrets[SecretAPIBase] = srv.URL + "/v1"
	oauthClient := NewClient(oauthStore)
	if _, err := oauthClient.StartLogin(context.Background(), "acc-2"); err != nil {
		t.Fatalf("start login: %v", err)
	}
	if err := oauthClient.CompleteLogin(context.Background(), "acc-2", "OOBCODE"); err != nil {
		t.Fatalf("pkce completion: %v", err)
	}
	oauthAdapter := oauthClient.OAuthAdapter()
	if oauthAdapter.ID != OAuthProviderID || oauthAdapter.Login == nil {
		t.Fatal("the PKCE entry must expose a login session")
	}

	// Both adapters run the identical chat + catalog code with their own key.
	apiResult, err := apiAdapter.Chat.ChatNonStream(context.Background(), "acc-1", chatRequest("deepseek/deepseek-v4-pro"))
	if err != nil {
		t.Fatalf("api-key chat: %v", err)
	}
	oauthResult, err := oauthAdapter.Chat.ChatNonStream(context.Background(), "acc-2", chatRequest("deepseek/deepseek-v4-pro"))
	if err != nil {
		t.Fatalf("pkce chat: %v", err)
	}
	if apiResult.Content != oauthResult.Content || apiResult.Content != "ok" {
		t.Fatalf("downstream results diverged: %+v vs %+v", apiResult, oauthResult)
	}
	apiModels, err := apiAdapter.Models.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("api-key catalog: %v", err)
	}
	oauthModels, err := oauthAdapter.Models.Models(context.Background(), "acc-2")
	if err != nil {
		t.Fatalf("pkce catalog: %v", err)
	}
	if len(apiModels) != len(oauthModels) || len(apiModels) == 0 {
		t.Fatalf("catalogs diverged: %d vs %d", len(apiModels), len(oauthModels))
	}
	if len(up.seenKeys) == 0 {
		t.Fatal("no downstream request was made")
	}
	for _, header := range up.seenKeys {
		if !strings.HasPrefix(header, "Bearer sk-orca-") {
			t.Fatalf("downstream must carry a plain bearer key, got %q", header)
		}
	}
	_ = fakeAuth
}

// Model discovery only ever talks to the inference origin and carries the
// catalog-declared capability metadata the selectors filter on.
func TestModelDiscoveryCarriesCapabilityMetadata(t *testing.T) {
	srv, up := newUpstream(t)
	client := NewClient(upstreamStore(t, srv))

	models, err := client.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	byID := map[string]providers.ModelInfo{}
	for _, model := range models {
		byID[model.PublicModel] = model
	}
	vision, ok := byID["anthropic/claude-opus-4.8"]
	if !ok {
		t.Fatalf("live model missing from catalog: %v", byID)
	}
	if !vision.Capabilities.ImageInput {
		t.Fatalf("image modality must come from architecture.input_modalities: %+v", vision.Capabilities)
	}
	if byID["deepseek/deepseek-v4-pro"].Capabilities.ImageInput {
		t.Fatal("a text-only model must not be advertised as image-input")
	}
	// The vendor/model namespace is preserved verbatim.
	if vision.NativeModel != "anthropic/claude-opus-4.8" {
		t.Fatalf("namespace was rewritten: %q", vision.NativeModel)
	}
	for _, path := range up.seenPaths {
		if !strings.HasPrefix(path, "/v1/") {
			t.Fatalf("catalog must be read from the inference origin, saw %q", path)
		}
	}
}

// Switching the requested capability recomputes the option set, and an image
// attachment removes every model that does not declare image input.
func TestCapabilityChangeRecomputesOptions(t *testing.T) {
	srv, _ := newUpstream(t)
	client := NewClient(upstreamStore(t, srv))

	chat, err := client.Models(context.Background(), "acc-1")
	if err != nil {
		t.Fatalf("chat catalog: %v", err)
	}
	if len(chat) != 2 {
		t.Fatalf("expected the two chat models, got %d", len(chat))
	}
	vision := FilterModels(chat, "vision")
	if len(vision) != 1 || vision[0].PublicModel != "anthropic/claude-opus-4.8" {
		t.Fatalf("vision filter wrong: %+v", vision)
	}
	embedding := FilterModels(chat, "embedding")
	if len(embedding) != 0 {
		t.Fatalf("embedding must not reuse the chat option set: %+v", embedding)
	}
}
