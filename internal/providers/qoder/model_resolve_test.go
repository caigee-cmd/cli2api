package qoder

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func resolveTestCatalog() []map[string]any {
	return []map[string]any{
		{"id": "auto", "mapped_key": "auto", "display_name": "Auto"},
		{"id": "qwen3.8-max", "mapped_key": "qmodel_38max", "display_name": "Qwen3.8-Max", "is_reasoning": true, "is_vl": false, "max_input_tokens": 256000},
		{"key": "gmodel", "display_name": "GLM-5.2"},
	}
}

func bridgeWithFetch(calls *int32) *Bridge {
	b := &Bridge{}
	b.catalogFetch = func(context.Context) ([]map[string]any, error) {
		atomic.AddInt32(calls, 1)
		return resolveTestCatalog(), nil
	}
	return b
}

// Cold cache: the request triggers one fetch and resolves the public name.
// Warm cache: the second request performs no fetch.
func TestResolveColdThenWarmCatalog(t *testing.T) {
	var calls int32
	b := bridgeWithFetch(&calls)
	for i := 0; i < 2; i++ {
		req, entry, err := resolveCatalogModel(context.Background(), b.RoutingCatalog, translate.ChatRequest{Model: "qwen3.8-max"})
		if err != nil || req.Model != "qmodel_38max" || entry == nil {
			t.Fatalf("got %q %v %v", req.Model, entry, err)
		}
	}
	if calls != 1 {
		t.Fatalf("fetches = %d, want 1 (warm cache must not fetch)", calls)
	}
}

func TestResolveDisplayNameToKey(t *testing.T) {
	var calls int32
	b := bridgeWithFetch(&calls)
	for _, name := range []string{"GLM-5.2", "glm-5.2", "gmodel", "Qwen3.8-Max", "qmodel_38max"} {
		req, _, err := resolveCatalogModel(context.Background(), b.RoutingCatalog, translate.ChatRequest{Model: name})
		if err != nil || (req.Model != "gmodel" && req.Model != "qmodel_38max") {
			t.Fatalf("%s -> %q %v", name, req.Model, err)
		}
	}
}

func TestResolveUnknownModelIsModelNotFound(t *testing.T) {
	var calls int32
	b := bridgeWithFetch(&calls)
	_, _, err := resolveCatalogModel(context.Background(), b.RoutingCatalog, translate.ChatRequest{Model: "no-such-model"})
	var perr *providers.Error
	if !errors.As(err, &perr) || perr.Code != "model_not_found" || perr.Status != 400 {
		t.Fatalf("err = %#v", err)
	}
}

func TestResolveAutoPassthrough(t *testing.T) {
	var calls int32
	b := bridgeWithFetch(&calls)
	req, _, err := resolveCatalogModel(context.Background(), b.RoutingCatalog, translate.ChatRequest{Model: "auto"})
	if err != nil || req.Model != "auto" {
		t.Fatalf("auto -> %q %v", req.Model, err)
	}
	b2 := &Bridge{catalogFetch: func(context.Context) ([]map[string]any, error) { return []map[string]any{{"key": "x"}}, nil }}
	req, _, err = resolveCatalogModel(context.Background(), b2.RoutingCatalog, translate.ChatRequest{Model: "auto"})
	if err != nil || req.Model != "auto" {
		t.Fatalf("auto without catalog entry -> %q %v", req.Model, err)
	}
}

// An unloadable catalog keeps the old pass-through behavior.
func TestResolveCatalogUnavailableFailsOpen(t *testing.T) {
	b := &Bridge{catalogFetch: func(context.Context) ([]map[string]any, error) { return nil, errors.New("down") }}
	req, _, err := resolveCatalogModel(context.Background(), b.RoutingCatalog, translate.ChatRequest{Model: "qwen3.8-max"})
	if err != nil || req.Model != "qwen3.8-max" {
		t.Fatalf("got %q %v", req.Model, err)
	}
}

func TestApplyCatalogEntryFillsModelConfig(t *testing.T) {
	req := translate.ChatRequest{Model: "qmodel_38max"}
	plain := BuildPlainChatBody(req, time.Unix(1700000000, 0))
	applyCatalogEntry(plain, req, resolveTestCatalog()[1])
	cfg := plain["model_config"].(map[string]any)
	if cfg["key"] != "qmodel_38max" || cfg["display_name"] != "Qwen3.8-Max" || cfg["is_vl"] != false || cfg["max_input_tokens"] != 256000 || cfg["is_reasoning"] != true {
		t.Fatalf("model_config = %v", cfg)
	}
	none := translate.ChatRequest{Model: "qmodel_38max", ReasoningEffort: []byte(`"none"`)}
	plain = BuildPlainChatBody(none, time.Unix(1700000000, 0))
	applyCatalogEntry(plain, none, resolveTestCatalog()[1])
	if plain["model_config"].(map[string]any)["is_reasoning"] != false {
		t.Fatal("explicit none must keep reasoning off")
	}
}
