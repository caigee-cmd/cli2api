package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type chatHTTPError = executor.PrepareError

type chatExecution struct {
	ctx            context.Context
	requestID      string
	started        time.Time
	request        translate.ChatRequest
	publicModel    string
	providerFilter string
	prefer         string
}

func (s *Server) prepareChatExecution(r *http.Request, request translate.ChatRequest) (chatExecution, error) {
	var identity auth.Identity
	if r != nil {
		identity = s.requestIdentity(r)
	}
	sessionHeader := ""
	prefer := ""
	ctx := context.Background()
	if r != nil {
		sessionHeader = r.Header.Get("X-CLI2API-Session")
		prefer = s.requestedAccount(r)
		ctx = r.Context()
	}
	var modelContexts executor.ModelContextStore
	if s.control != nil && s.control.Settings != nil {
		modelContexts = s.control.Settings
	}
	var catalogs executor.CatalogPreparer
	if s.manager != nil {
		catalogs = s.manager
	}
	var logs executor.RequestStarter
	if s.recorder != nil {
		logs = s.recorder
	}
	got, err := s.executor.Prepare(executor.PrepareInput{
		Context:           ctx,
		Request:           request,
		Identity:          identity,
		PreferAccount:     prefer,
		SessionHeader:     sessionHeader,
		CrossProviderPool: s.crossProviderModelPool.Load(),
		ModelContexts:     modelContexts,
		Catalogs:          catalogs,
		Logs:              logs,
	})
	if err != nil {
		return chatExecution{}, err
	}
	return chatExecution{
		ctx:            got.Context,
		requestID:      got.RequestID,
		started:        got.Started,
		request:        got.Request,
		publicModel:    got.PublicModel,
		providerFilter: got.ProviderFilter,
		prefer:         got.Prefer,
	}, nil
}

func writeChatHTTPError(w http.ResponseWriter, err error) {
	var requestErr *chatHTTPError
	if errors.As(err, &requestErr) {
		writeErr(w, requestErr.Status, requestErr.Code, requestErr.Message)
		return
	}
	writeClassifiedErr(w, err)
}

func (s *Server) rejectsBareModel(model string) bool {
	poolOn := s != nil && s.crossProviderModelPool.Load()
	return executor.RejectsBareModel(model, poolOn)
}

func (s *Server) resolveProviderFilter(req *translate.ChatRequest) string {
	poolOn := s != nil && s.crossProviderModelPool.Load()
	return executor.ResolveProviderFilter(req, poolOn)
}

func (s *Server) applyPinnedProviderFilter(providerFilter, publicModel, prefer string) string {
	if s == nil {
		return providerFilter
	}
	return executor.ApplyPinnedProviderFilter(s.pool, providerFilter, publicModel, prefer)
}

func (s *Server) applyModelContextDefaults(ctx context.Context, req *translate.ChatRequest, providerFilter string) error {
	var store executor.ModelContextStore
	if s != nil && s.control != nil {
		store = s.control.Settings
	}
	return executor.ApplyModelContextDefaults(ctx, store, req, providerFilter)
}

func requestSessionKey(r *http.Request, identity auth.Identity, req translate.ChatRequest) string {
	header := ""
	if r != nil {
		header = r.Header.Get("X-CLI2API-Session")
	}
	return executor.SessionKeyFor(header, identity, req)
}
