package api

import (
	"testing"

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
	s.cfg.CrossProviderModelPool = true
	req = modelChatRequest("glm-5.2")
	if got := s.resolveProviderFilter(req); got != "" || req.Model != "glm-5.2" {
		t.Fatalf("cross-provider bare filter=%s model=%s", got, req.Model)
	}
}
