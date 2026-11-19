package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	appsvc "github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

const (
	maxProbeModels         = 100
	maxProbeBatchDuration  = 10 * time.Minute
	maxConcurrentProbeJobs = 4
)

type modelProbeRequest struct {
	Models []string `json:"models"`
}

type modelProbeSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

type modelProbeResponse struct {
	Results []executorProbeResult `json:"results"`
	Summary modelProbeSummary     `json:"summary"`
	Elapsed int                   `json:"elapsed_ms"`
}

type executorProbeResult = executor.ProbeModelResult

// HandleModelProbe checks a set of models with one minimal live chat request
// each. It returns 200 with per-model results; a failed model is a normal
// probe outcome, not a transport error for the whole batch.
func (h *Handler) HandleModelProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	if h == nil || h.Executor == nil {
		writeErr(w, http.StatusServiceUnavailable, "probe_unavailable", "model probing is not configured")
		return
	}
	var input modelProbeRequest
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil && !errors.Is(err, io.EOF) {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	models := trimSpaceSlice(input.Models)
	if len(models) == 0 {
		// Cache-only enumeration: probing must never start a catalog fetch or
		// background refresh. Merge view is the persisted/restored snapshot, so
		// it is the stable source of model IDs for a diagnostic batch.
		catalog := h.peekDisplayModels("", appsvc.CatalogModeMerge)
		for _, entry := range catalog {
			if id, ok := entry["id"].(string); ok && strings.TrimSpace(id) != "" {
				models = append(models, strings.TrimSpace(id))
			}
		}
	}
	models = uniqueStrings(models)
	if len(models) == 0 {
		writeErr(w, http.StatusBadRequest, "no_models", "no models available to probe")
		return
	}
	if len(models) > maxProbeModels {
		writeErr(w, http.StatusBadRequest, "too_many_models", fmt.Sprintf("at most %d models can be checked at once", maxProbeModels))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), maxProbeBatchDuration)
	defer cancel()
	started := time.Now()
	results := make([]executorProbeResult, len(models))
	sem := make(chan struct{}, maxConcurrentProbeJobs)
	var wg sync.WaitGroup
	for i, model := range models {
		// Do not start new work once the operator has disconnected or the
		// batch deadline has passed.
		if ctx.Err() != nil {
			results[i] = executorProbeResult{Model: model, Code: "probe_canceled", Kind: "canceled", Message: "probe batch canceled"}
			continue
		}
		wg.Add(1)
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Done()
			results[i] = executorProbeResult{Model: model, Code: "probe_canceled", Kind: "canceled", Message: "probe batch canceled"}
			continue
		}
		go func(i int, model string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = executorProbeResult(h.Executor.ProbeModel(ctx, model))
		}(i, model)
	}
	wg.Wait()

	out := modelProbeResponse{
		Results: results,
		Summary: modelProbeSummary{Total: len(results)},
		Elapsed: int(time.Since(started).Milliseconds()),
	}
	for _, res := range results {
		switch {
		case res.OK:
			out.Summary.Passed++
		case strings.EqualFold(strings.TrimSpace(res.Code), "no_models") || strings.EqualFold(strings.TrimSpace(res.Message), "model required"):
			out.Summary.Skipped++
		default:
			out.Summary.Failed++
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func trimSpaceSlice(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, strings.TrimSpace(item))
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
