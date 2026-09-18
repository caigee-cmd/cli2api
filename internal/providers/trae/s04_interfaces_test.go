package trae

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

var _ Store = (*accounts.Store)(nil)
var _ SecretReader = (*accounts.Store)(nil)
var _ ModelSettingReader = (*accounts.Store)(nil)
var _ ModelMaxModeReader = (*accounts.Store)(nil)

type requiredStore struct {
	items map[string][]byte
}

func (s requiredStore) Get(context.Context, string) (accounts.Account, error) {
	return accounts.Account{Provider: "trae", ProviderRegion: "cn"}, nil
}
func (s requiredStore) LoadCredentialPayload(_ context.Context, accountID string) (string, []byte, error) {
	payload, ok := s.items[accountID]
	if !ok {
		return "", nil, accounts.ErrAccountNotFound
	}
	return CredentialFormat, payload, nil
}
func (requiredStore) SaveCredentialPayload(context.Context, string, string, []byte) error {
	return nil
}
func (requiredStore) Observe(context.Context, string, string, string, string, string) error {
	return nil
}

type maxModeOnlyStore struct {
	requiredStore
	lookups []string
}

func (s *maxModeOnlyStore) GetProviderModelMaxMode(_ context.Context, provider, modelID string) (bool, error) {
	s.lookups = append(s.lookups, provider+"/"+modelID)
	return true, nil
}

func TestMissingSecretReaderSkipsGlobalProxy(t *testing.T) {
	client := NewClient(requiredStore{})
	got, err := client.globalProxy(context.Background())
	if err != nil {
		t.Fatalf("missing SecretReader must skip, not fail: %v", err)
	}
	if got != "" {
		t.Fatalf("missing SecretReader returned %q", got)
	}
}

func TestMissingModelSettingReaderFallsBackToMaxMode(t *testing.T) {
	payload, _ := Credential{AccessToken: "at", RefreshToken: "rt", UID: "u1", Domain: DomainCN, ExpiresAt: 4102444800}.Encode()
	store := &maxModeOnlyStore{requiredStore: requiredStore{items: map[string][]byte{"acc1": payload}}}
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == pathModels {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"config_info_list": []map[string]any{{
					"config_name":           "DeepSeek-V4-Pro-Official",
					"context_window_tokens": map[string]any{"dev": 200000, "max": 1000000},
					"display_config":        map[string]any{"display_name": "DeepSeek-V4-Pro 正式版"},
				}},
			})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(soloSSE))
	}))
	defer server.Close()
	client := NewClient(store)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{server: server.URL, round: server.Client().Transport}

	if _, err := client.ChatNonStream(context.Background(), "acc1", translate.ChatRequest{
		Model: "DeepSeek-V4-Pro-Official",
	}); err != nil {
		t.Fatal(err)
	}
	if got["is_max_mode"] != float64(1) {
		t.Fatalf("legacy max-mode fallback not applied: %v", got)
	}
	if len(store.lookups) == 0 || store.lookups[0] != "trae/deepseek-v4-pro-official" {
		t.Fatalf("lookup key mismatch: %v", store.lookups)
	}
}
