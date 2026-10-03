package orcarouter

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// Live acceptance against the real OrcaRouter gateway, driven through this
// package's own provider code rather than a bare HTTP call. It is skipped when
// no integration key is present, so the ordinary suite stays hermetic.
//
// Run it with:
//
//	ORCAROUTER_API_KEY=sk-orca-… go test ./internal/providers/orcarouter/ -run TestLive -v
func liveClient(t *testing.T) (*Client, string) {
	t.Helper()
	key := os.Getenv("ORCAROUTER_API_KEY")
	if key == "" {
		t.Skip("ORCAROUTER_API_KEY is not set; live provider acceptance skipped")
	}
	return newLiveClient(t, key), "acc-live"
}

// newLiveClient builds a fresh provider client for one account. Each attempt gets
// its own client so a key-scope rejection elsewhere cannot mark the shared
// account as needing reauthentication and mask the model that would work.
func newLiveClient(t *testing.T, key string) *Client {
	t.Helper()
	store := newFakeStore()
	payload, err := Credential{Format: CredentialFormat, APIKey: key}.Encode()
	if err != nil {
		t.Fatalf("encode credential: %v", err)
	}
	store.format, store.payload = CredentialFormat, payload
	return NewClient(store)
}

// The pasted-key entry reaches the documented inference origin and the catalog
// path the console selectors consume, and the capability filters run on what the
// live catalog actually declared.
func TestLiveCatalogThroughProvider(t *testing.T) {
	client, account := liveClient(t)
	models, err := client.Models(context.Background(), account)
	if err != nil {
		t.Fatalf("live models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("live catalog returned no models")
	}
	var withImage int
	for _, model := range models {
		// Ids keep the upstream vendor/model namespace verbatim.
		if model.PublicModel == "" || model.NativeModel == "" {
			t.Fatalf("catalog entry lost its id: %+v", model)
		}
		if model.Capabilities.ImageInput {
			withImage++
		}
	}
	if withImage == 0 {
		t.Fatal("live catalog declared no image-input chat model")
	}

	// The vision list is a strict, non-empty subset of the chat list, and every
	// member declared image input.
	vision := FilterModels(models, CapabilityVision)
	if len(vision) == 0 || len(vision) >= len(models) {
		t.Fatalf("vision filter is not a proper subset: %d of %d", len(vision), len(models))
	}
	for _, model := range vision {
		if !model.Capabilities.ImageInput {
			t.Fatalf("vision filter admitted a model without image input: %s", model.PublicModel)
		}
	}
	// A text-only model must never leak into the vision list.
	for _, model := range models {
		if !model.Capabilities.ImageInput && containsModel(vision, model.PublicModel) {
			t.Fatalf("text-only model %s leaked into the vision list", model.PublicModel)
		}
	}
}

// A real chat completion through the pasted-key adapter: the request travels
// the same Client path the gateway uses, against the live API origin.
//
// An integration key is scoped to a subset of the catalog, so the test walks the
// live list and uses the first model the key is actually allowed to call. A
// scope denial (`403 model_access_denied`) is a key-permission fact, not a
// provider bug, so it moves on instead of failing; only "no model at all was
// callable" is a failure.
func TestLiveChatThroughProvider(t *testing.T) {
	client, account := liveClient(t)
	ctx := context.Background()
	models, err := client.Models(ctx, account)
	if err != nil {
		t.Fatalf("live models: %v", err)
	}
	if len(models) == 0 {
		t.Fatal("live catalog returned no models")
	}
	var lastErr error
	for _, model := range models {
		// A fresh client per attempt: a key-scope rejection on one model must not
		// leave the account marked for reauthentication on the next.
		attempt := newLiveClient(t, os.Getenv("ORCAROUTER_API_KEY"))
		outcome, err := attempt.ChatNonStream(ctx, account, translate.ChatRequest{
			Model:    model.PublicModel,
			Messages: []translate.ChatMessage{{Role: "user", Content: "Reply with the single word: pong"}},
		})
		if err != nil {
			if isModelScopeDenial(err) {
				lastErr = err
				continue
			}
			t.Fatalf("live chat on %s: %v", model.PublicModel, err)
		}
		if outcome.Content == "" && outcome.Reasoning == "" && outcome.FinishReason == "" {
			t.Fatalf("live chat on %s returned an empty completion", model.PublicModel)
		}
		return
	}
	t.Fatalf("no catalog model was callable with this key: %v", lastErr)
}

// isModelScopeDenial reports the gateway's key-scope rejection, which says the
// key may not use a model rather than that the provider path is broken.
func isModelScopeDenial(err error) bool {
	return err != nil && strings.Contains(err.Error(), "model_access_denied")
}

func containsModel(models []providers.ModelInfo, id string) bool {
	for _, model := range models {
		if model.PublicModel == id {
			return true
		}
	}
	return false
}
