package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/control"
)

// TestModelSettingResponseKimiCarriesReasoningOnly pins the PATCH/GET payload
// shape for Kimi Code: the console writes and reads a reasoning default, and
// there is no max-mode tier to report.
func TestModelSettingResponseKimiCarriesReasoningOnly(t *testing.T) {
	response := modelSettingResponse(control.ModelSetting{
		Provider:        "kimi",
		Model:           "kimi-for-coding",
		ReasoningEffort: "max",
		ContextCustom:   true,
	})
	if response["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %#v", response["reasoning_effort"])
	}
	if response["context_custom"] != true {
		t.Fatalf("context_custom = %#v", response["context_custom"])
	}
	if response["provider"] != "kimi" || response["model"] != "kimi-for-coding" {
		t.Fatalf("identity = %#v", response)
	}
	if _, ok := response["max_mode"]; ok {
		t.Fatal("kimi has no max-mode switch to report")
	}
}

func TestModelSettingResponseCodeArtsCarriesReasoningOnly(t *testing.T) {
	response := modelSettingResponse(control.ModelSetting{
		Provider:        "codearts",
		Model:           "glm-5.2",
		ReasoningEffort: "max",
		ContextCustom:   true,
	})
	if response["reasoning_effort"] != "max" {
		t.Fatalf("reasoning_effort = %#v", response["reasoning_effort"])
	}
	if response["context_custom"] != true {
		t.Fatalf("context_custom = %#v", response["context_custom"])
	}
	if response["provider"] != "codearts" || response["model"] != "glm-5.2" {
		t.Fatalf("identity = %#v", response)
	}
	if _, ok := response["max_mode"]; ok {
		t.Fatal("codearts has no max-mode switch to report")
	}
}

func TestHandleModelsAPIPaginatesOptionalPage(t *testing.T) {
	h := &Handler{FetchDisplayModels: func(bool, string, control.CatalogMode) ([]map[string]any, error) {
		models := make([]map[string]any, 0, 7)
		for i := 0; i < 7; i++ {
			models = append(models, map[string]any{"id": "model-" + string(rune('0'+i))})
		}
		return models, nil
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/models?page=2&page_size=3", nil)
	rec := httptest.NewRecorder()
	h.HandleModelsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data       []map[string]any `json:"data"`
		Pagination struct {
			Page       int  `json:"page"`
			PageSize   int  `json:"page_size"`
			Total      int  `json:"total"`
			TotalPages int  `json:"total_pages"`
			HasNext    bool `json:"has_next"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 3 || body.Data[0]["id"] != "model-3" || body.Data[2]["id"] != "model-5" {
		t.Fatalf("data = %#v", body.Data)
	}
	if body.Pagination.Page != 2 || body.Pagination.PageSize != 3 || body.Pagination.Total != 7 || body.Pagination.TotalPages != 3 || !body.Pagination.HasNext {
		t.Fatalf("pagination = %+v", body.Pagination)
	}
}

func TestHandleModelsAPIDefaultRemainsUnpaginated(t *testing.T) {
	called := atomic.Int32{}
	h := &Handler{FetchDisplayModels: func(bool, string, control.CatalogMode) ([]map[string]any, error) {
		called.Add(1)
		return []map[string]any{{"id": "first"}, {"id": "second"}}, nil
	}}
	rec := httptest.NewRecorder()
	h.HandleModelsAPI(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK || called.Load() != 1 {
		t.Fatalf("status = %d calls = %d", rec.Code, called.Load())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body["data"].([]any)) != 2 {
		t.Fatalf("body = %#v", body)
	}
	if _, ok := body["pagination"]; ok {
		t.Fatalf("unexpected pagination = %#v", body)
	}
}

func TestHandleModelsAPIKeepsSyntheticAggregationOutOfConsoleCatalog(t *testing.T) {
	store := &consoleAggregationStore{list: []accounts.Aggregation{{
		ID: "deepseek-v4.1-flash", Enabled: true,
		Members: []accounts.AggregationMember{{Provider: "trae", Model: "deepseek-v4.1-flash"}},
	}}}
	var pool atomic.Bool
	pool.Store(true)
	h := &Handler{
		Control:           &control.Services{Aggregations: control.NewAggregations(store)},
		CrossProviderPool: &pool,
		FetchDisplayModels: func(bool, string, control.CatalogMode) ([]map[string]any, error) {
			return []map[string]any{{
				"id": "deepseek-v4.1-flash", "provider": "trae", "owned_by": "trae",
			}}, nil
		},
	}
	rec := httptest.NewRecorder()
	h.HandleModelsAPI(rec, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("data = %#v", body.Data)
	}
	if body.Data[0]["provider"] != "trae" || body.Data[0]["id"] != "deepseek-v4.1-flash" {
		t.Fatalf("real provider row was replaced: %#v", body.Data[0])
	}
}

type consoleAggregationStore struct {
	list []accounts.Aggregation
}

func (s *consoleAggregationStore) InsertAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	s.list = append(s.list, agg)
	return agg, nil
}

func (s *consoleAggregationStore) SaveAggregation(_ context.Context, agg accounts.Aggregation) (accounts.Aggregation, error) {
	for i := range s.list {
		if s.list[i].ID == agg.ID {
			s.list[i] = agg
		}
	}
	return agg, nil
}

func (s *consoleAggregationStore) ListAggregations(context.Context) ([]accounts.Aggregation, error) {
	return s.list, nil
}

func (s *consoleAggregationStore) GetAggregation(_ context.Context, id string) (accounts.Aggregation, error) {
	for _, agg := range s.list {
		if agg.ID == id {
			return agg, nil
		}
	}
	return accounts.Aggregation{}, accounts.ErrAggregationNotFound
}

func (s *consoleAggregationStore) DeleteAggregation(_ context.Context, id string) error {
	for i := range s.list {
		if s.list[i].ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return nil
		}
	}
	return accounts.ErrAggregationNotFound
}

func TestHandleModelsAPIRejectsInvalidPagination(t *testing.T) {
	h := &Handler{FetchDisplayModels: func(bool, string, control.CatalogMode) ([]map[string]any, error) {
		return []map[string]any{{"id": "model"}}, nil
	}}
	rec := httptest.NewRecorder()
	h.HandleModelsAPI(rec, httptest.NewRequest(http.MethodGet, "/api/models?page_size=101", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
