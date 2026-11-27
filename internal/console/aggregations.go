package console

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type aggregationMemberInput struct {
	Provider  string `json:"provider"`
	Region    string `json:"region"`
	Model     string `json:"model"`
	AccountID string `json:"account_id"`
	Priority  int    `json:"priority"`
	Weight    int    `json:"weight"`
}

func decodeAggregationMembers(input []aggregationMemberInput) []accounts.AggregationMember {
	members := make([]accounts.AggregationMember, 0, len(input))
	for _, m := range input {
		members = append(members, accounts.AggregationMember{
			Provider: m.Provider, Region: m.Region, Model: m.Model, AccountID: m.AccountID,
			Priority: m.Priority, Weight: m.Weight,
		})
	}
	return members
}

func writeAggregationError(w http.ResponseWriter, err error, _ string) {
	if errors.Is(err, accounts.ErrAggregationNotFound) {
		writeErr(w, http.StatusNotFound, "aggregation_not_found", err.Error())
		return
	}
	// control wraps validation failures as OperationError("invalid_request"),
	// which writeOperationError already maps to 400.
	writeOperationError(w, err)
}

func (h *Handler) HandleAggregations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := h.Control.Aggregations.List(r.Context())
		if err != nil {
			writeAggregationError(w, err, "list")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": list})
	case http.MethodPost:
		var input struct {
			ID               string                   `json:"id"`
			Name             string                   `json:"name"`
			Enabled          *bool                    `json:"enabled"`
			Strategy         string                   `json:"strategy"`
			AllowFallbacks   *bool                    `json:"allow_fallbacks"`
			MaxAttempts      int                      `json:"max_attempts"`
			ContextLength    int                      `json:"context_length"`
			MaxOutputTokens  int                      `json:"max_output_tokens"`
			ReasoningOptions []string                 `json:"reasoning_options"`
			ReasoningEffort  string                   `json:"reasoning_effort"`
			Members          []aggregationMemberInput `json:"members"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		allowFallbacks := true
		if input.AllowFallbacks != nil {
			allowFallbacks = *input.AllowFallbacks
		}
		agg, err := h.Control.Aggregations.Create(r.Context(), accounts.CreateAggregation{
			ID:               input.ID,
			Name:             input.Name,
			Enabled:          enabled,
			Strategy:         accounts.AggregationStrategy(input.Strategy),
			AllowFallbacks:   allowFallbacks,
			MaxAttempts:      input.MaxAttempts,
			ContextLength:    input.ContextLength,
			MaxOutputTokens:  input.MaxOutputTokens,
			ReasoningOptions: input.ReasoningOptions,
			ReasoningEffort:  input.ReasoningEffort,
			Members:          decodeAggregationMembers(input.Members),
		})
		if err != nil {
			writeAggregationError(w, err, "create")
			return
		}
		writeJSON(w, http.StatusCreated, agg)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
}

func (h *Handler) HandleAggregationByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/aggregations/"), "/")
	if id == "" {
		writeErr(w, http.StatusNotFound, "aggregation_not_found", "aggregation id required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		agg, err := h.Control.Aggregations.Get(r.Context(), id)
		if err != nil {
			writeAggregationError(w, err, "get")
			return
		}
		writeJSON(w, http.StatusOK, agg)
	case http.MethodPatch:
		var input struct {
			Name             *string                   `json:"name"`
			Enabled          *bool                     `json:"enabled"`
			Strategy         *string                   `json:"strategy"`
			AllowFallbacks   *bool                     `json:"allow_fallbacks"`
			MaxAttempts      *int                      `json:"max_attempts"`
			ContextLength    *int                      `json:"context_length"`
			MaxOutputTokens  *int                      `json:"max_output_tokens"`
			ReasoningOptions *[]string                 `json:"reasoning_options"`
			ReasoningEffort  *string                   `json:"reasoning_effort"`
			Members          *[]aggregationMemberInput `json:"members"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		var strategy *accounts.AggregationStrategy
		if input.Strategy != nil {
			value := accounts.AggregationStrategy(*input.Strategy)
			strategy = &value
		}
		var members *[]accounts.AggregationMember
		if input.Members != nil {
			decoded := decodeAggregationMembers(*input.Members)
			members = &decoded
		}
		agg, err := h.Control.Aggregations.Update(r.Context(), id, accounts.UpdateAggregation{
			Name: input.Name, Enabled: input.Enabled, Strategy: strategy,
			AllowFallbacks: input.AllowFallbacks, MaxAttempts: input.MaxAttempts,
			ContextLength: input.ContextLength, MaxOutputTokens: input.MaxOutputTokens,
			ReasoningOptions: input.ReasoningOptions, ReasoningEffort: input.ReasoningEffort,
			Members: members,
		})
		if err != nil {
			writeAggregationError(w, err, "update")
			return
		}
		writeJSON(w, http.StatusOK, agg)
	case http.MethodDelete:
		if err := h.Control.Aggregations.Delete(r.Context(), id); err != nil {
			writeAggregationError(w, err, "delete")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET, PATCH, or DELETE only")
	}
}
