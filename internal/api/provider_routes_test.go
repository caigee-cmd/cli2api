package api

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func modelChatRequest(model string) *translate.ChatRequest {
	return &translate.ChatRequest{Model: model}
}

func TestProviderPrefixedModelsStayDistinct(t *testing.T) {
	if modelContextKey("glm-5.2") == modelContextKey("qoder/glm-5.2") {
		t.Fatal("bare and provider-prefixed models must not share context settings")
	}
	if modelContextKey("qoder/glm-5.2") == modelContextKey("workbuddy/glm-5.2") {
		t.Fatal("provider-prefixed models must not share context settings")
	}
}

func TestResolveProviderFilterPinsFamilyAndKeepsBareID(t *testing.T) {
	s := &Server{}
	req := modelChatRequest("qoder/glm-5.2")
	if got := s.resolveProviderFilter(req); got != "qoder" || req.Model != "glm-5.2" {
		t.Fatalf("filter=%s model=%s", got, req.Model)
	}
	req = modelChatRequest("workbuddy/glm-5.2")
	if got := s.resolveProviderFilter(req); got != "workbuddy" || req.Model != "glm-5.2" {
		t.Fatalf("filter=%s model=%s", got, req.Model)
	}
	req = modelChatRequest("trae/glm-5.2")
	if got := s.resolveProviderFilter(req); got != "trae" || req.Model != "glm-5.2" {
		t.Fatalf("filter=%s model=%s", got, req.Model)
	}
	req = modelChatRequest("glm-5.2")
	if got := s.resolveProviderFilter(req); got != "qoder" || req.Model != "glm-5.2" {
		t.Fatalf("bare model filter=%s model=%s", got, req.Model)
	}
	s.crossProviderModelPool.Store(true)
	req = modelChatRequest("glm-5.2")
	if got := s.resolveProviderFilter(req); got != "" || req.Model != "glm-5.2" {
		t.Fatalf("cross-provider bare filter=%s model=%s", got, req.Model)
	}
}

func TestResolveProviderFilterFallsBackToSoleFamily(t *testing.T) {
	pool := accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	s := &Server{pool: pool}

	req := modelChatRequest("glm-5.2")
	if got := s.resolveProviderFilter(req); got != "workbuddy" || req.Model != "glm-5.2" {
		t.Fatalf("workbuddy-only bare filter=%s model=%s", got, req.Model)
	}

	pool.Upsert(accounts.Item{ID: "q1", URL: "http://q1", Provider: "qoder", Runtime: "child_process"})
	req = modelChatRequest("glm-5.2")
	if got := s.resolveProviderFilter(req); got != "qoder" || req.Model != "glm-5.2" {
		t.Fatalf("mixed pool bare filter=%s model=%s", got, req.Model)
	}

	pool = accounts.NewPool(nil, nil)
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	pool.Upsert(accounts.Item{ID: "t1", Provider: "trae", Runtime: "in_process"})
	s.pool = pool
	req = modelChatRequest("glm-5.2")
	if got := s.resolveProviderFilter(req); got != "qoder" || req.Model != "glm-5.2" {
		t.Fatalf("multi non-qoder bare filter=%s model=%s", got, req.Model)
	}
}

func TestApplyPinnedProviderFilterOverridesBareDefault(t *testing.T) {
	pool := accounts.NewPool([]string{"http://q1"}, []string{"q1"})
	pool.Upsert(accounts.Item{ID: "wb1", Provider: "workbuddy", Runtime: "in_process"})
	s := &Server{pool: pool}

	if got := s.applyPinnedProviderFilter("qoder", "glm-5.2", "wb1"); got != "workbuddy" {
		t.Fatalf("bare pin override=%s", got)
	}
	if got := s.applyPinnedProviderFilter("workbuddy", "workbuddy/glm-5.2", "q1"); got != "workbuddy" {
		t.Fatalf("prefixed model must keep forced family, got=%s", got)
	}
	if got := s.applyPinnedProviderFilter("qoder", "glm-5.2", "missing"); got != "qoder" {
		t.Fatalf("unknown pin must keep filter, got=%s", got)
	}
	if got := s.applyPinnedProviderFilter("", "glm-5.2", "wb1"); got != "" {
		t.Fatalf("empty cross-provider filter must stay empty, got=%s", got)
	}
}
