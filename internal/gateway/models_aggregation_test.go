package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/auth"
)

func TestHandleModelsUsesAggregationKeyList(t *testing.T) {
	called := false
	h := &Handler{
		Models: func(bool, string) ([]map[string]any, error) {
			return []map[string]any{{"id": "kimi-k2.6", "object": "model", "provider": "qoder"}}, nil
		},
		FilterModels: func(_ *http.Request, models []map[string]any) []map[string]any { return models },
		AggregationKeyModels: func(_ context.Context, _ *http.Request, models []map[string]any) []map[string]any {
			called = true
			if len(models) != 1 || models[0]["id"] != "kimi-k2.6" {
				t.Fatalf("aggregation callback input=%v", models)
			}
			return []map[string]any{{"id": "glm-5.2", "object": "model", "owned_by": "aggregation"}}
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{Kind: auth.KindKey, ModelAggregation: true}))
	rec := httptest.NewRecorder()
	h.HandleModels(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("aggregation callback was not used")
	}
	if len(body.Data) != 1 || body.Data[0]["id"] != "glm-5.2" {
		t.Fatalf("data=%v", body.Data)
	}
	for _, model := range body.Data {
		if model["provider"] != nil || model["native_model"] != nil {
			t.Fatalf("provider metadata leaked: %v", model)
		}
	}
}

func TestHandleModelsAggregationKeyWithoutCallbackKeepsPrefixOff(t *testing.T) {
	h := &Handler{
		Models: func(bool, string) ([]map[string]any, error) {
			return []map[string]any{{"id": "kimi-k2.6", "object": "model", "provider": "qoder"}}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{Kind: auth.KindKey, ModelAggregation: true}))
	rec := httptest.NewRecorder()
	h.HandleModels(rec, req)
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0]["id"] != "kimi-k2.6" {
		t.Fatalf("data=%v", body.Data)
	}
}
