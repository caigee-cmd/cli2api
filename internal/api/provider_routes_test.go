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
	s.crossProviderModelPool.Store(true)
	req = modelChatRequest("glm-5.2")
	if got := s.resolveProviderFilter(req); got != "" || req.Model != "glm-5.2" {
		t.Fatalf("cross-provider bare filter=%s model=%s", got, req.Model)
	}
}

func TestDisabledCrossProviderModelPoolRejectsBareModels(t *testing.T) {
	s := &Server{}
	if !s.rejectsBareModel("glm-5.2") {
		t.Fatal("bare model must require a provider prefix when the pool is disabled")
	}
	for _, model := range []string{"qoder/glm-5.2", "workbuddy/glm-5.2", "trae/glm-5.2"} {
		if s.rejectsBareModel(model) {
			t.Fatalf("prefixed model must remain allowed: %s", model)
		}
	}
	s.crossProviderModelPool.Store(true)
	if s.rejectsBareModel("glm-5.2") {
		t.Fatal("bare model must be allowed when the pool is enabled")
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
