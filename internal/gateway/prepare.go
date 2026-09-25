package gateway

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

type Execution struct {
	Context            context.Context
	RequestID          string
	Started            time.Time
	Request            translate.ChatRequest
	NativeRequest      *translate.NativeResponsesRequest
	UseNativeResponses bool
	PublicModel        string
	ProviderFilter     string
	Prefer             string
	// ResponseToolNames restores namespaced Responses tool identities on the
	// reply. Gateway-local: it is never handed to the executor or a provider.
	ResponseToolNames map[string]translate.ResponseToolName
}

func (h *Handler) PrepareNativeResponsesExecution(r *http.Request, native *translate.NativeResponsesRequest, compat translate.ChatRequest) (Execution, error) {
	if native == nil {
		return Execution{}, &chatHTTPError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "responses request required"}
	}
	return h.prepareExecution(r, compat, native)
}

func (h *Handler) PrepareChatExecution(r *http.Request, request translate.ChatRequest) (Execution, error) {
	return h.prepareExecution(r, request, nil)
}

func (h *Handler) prepareExecution(r *http.Request, request translate.ChatRequest, native *translate.NativeResponsesRequest) (Execution, error) {
	var identity auth.Identity
	if r != nil {
		identity = h.requestIdentity(r)
	}
	sessionHeader := ""
	prefer := ""
	ctx := context.Background()
	if r != nil {
		sessionHeader = r.Header.Get("X-CLI2API-Session")
		prefer = h.requestedAccount(r)
		ctx = r.Context()
	}
	var logs executor.RequestStarter
	if h != nil && h.Logs != nil {
		logs = h.Logs
	} else if h != nil && h.Recorder != nil {
		logs = h.Recorder
	}
	var modelContexts executor.ModelContextStore
	if h != nil {
		modelContexts = h.ModelContexts
	}
	var catalogs executor.CatalogPreparer
	if h != nil {
		catalogs = h.Catalogs
	}
	got, err := h.Executor.Prepare(executor.PrepareInput{
		Context:       ctx,
		Request:       request,
		NativeRequest: native,
		Identity:      identity,

		PreferAccount:     prefer,
		SessionHeader:     sessionHeader,
		CrossProviderPool: h.crossProviderPoolOn(),
		ModelContexts:     modelContexts,
		Catalogs:          catalogs,
		Logs:              logs,
	})
	if err != nil {
		return Execution{}, err
	}
	return Execution{
		Context:       got.Context,
		RequestID:     got.RequestID,
		Started:       got.Started,
		Request:       got.Request,
		NativeRequest: got.NativeRequest,
		PublicModel:   got.PublicModel,

		ProviderFilter: got.ProviderFilter,
		Prefer:         got.Prefer,
	}, nil
}

func writeChatHTTPError(w http.ResponseWriter, err error) {
	var requestErr *chatHTTPError
	if errors.As(err, &requestErr) {
		writeErr(w, requestErr.Status, requestErr.Code, requestErr.Message)
		return
	}
	WriteClassifiedErr(w, err)
}
