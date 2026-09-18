package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type chatHTTPError struct {
	Status  int
	Code    string
	Message string
}

func (e *chatHTTPError) Error() string { return e.Message }

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
	publicModel := request.Model
	if s.rejectsBareModel(publicModel) {
		return chatExecution{}, &chatHTTPError{Status: http.StatusBadRequest, Code: "provider_prefix_required", Message: "cross-provider model pool is disabled; use a provider-prefixed model ID such as qoder/glm-5.2"}
	}
	providerFilter := s.resolveProviderFilter(&request)
	prefer := s.requestedAccount(r)
	providerFilter = s.applyPinnedProviderFilter(providerFilter, publicModel, prefer)
	identity := s.requestIdentity(r)
	if providerFilter != "" && !identity.AllowsProvider(providerFilter) {
		return chatExecution{}, &chatHTTPError{Status: http.StatusForbidden, Code: "provider_not_allowed", Message: "This API key cannot use provider " + providerFilter}
	}
	if err := s.applyModelContextDefaults(r.Context(), &request, providerFilter); err != nil {
		return chatExecution{}, &chatHTTPError{Status: http.StatusInternalServerError, Code: "model_setting_failed", Message: err.Error()}
	}
	if s.manager != nil {
		s.manager.EnsureModelCatalogs(r.Context(), false)
	}
	requestID := accounts.NewRequestID()
	started := time.Now().UTC()
	s.startRequestLog(accounts.RequestLog{
		ID: requestID, CreatedAt: started, Stream: request.Stream, Status: accounts.RequestStatusStarted,
		RequestedModel: firstNonEmpty(publicModel, request.Model),
		MessageCount:   len(request.Messages), EmptyMessageIndexes: translate.EmptyMessageIndexes(request.Messages),
		MessageRoles: translate.MessageRoles(request.Messages),
	})
	ctx := executor.WithAllowedProviders(executor.WithRequestID(r.Context(), requestID), identity.AllowedProviders)
	if sessionKey := requestSessionKey(r, identity, request); sessionKey != "" {
		ctx = executor.WithSessionKey(ctx, sessionKey)
	}
	return chatExecution{
		ctx: ctx, requestID: requestID, started: started, request: request,
		publicModel: publicModel, providerFilter: providerFilter, prefer: prefer,
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

func requestSessionKey(r *http.Request, identity auth.Identity, req translate.ChatRequest) string {
	raw := ""
	kind := "content"
	if r != nil {
		raw = strings.TrimSpace(r.Header.Get("X-CLI2API-Session"))
		if raw != "" {
			kind = "header"
		}
	}
	if raw == "" {
		raw = translate.ContentSessionSeed(req)
	}
	if raw == "" {
		return ""
	}
	namespace := "console"
	if identity.Kind == auth.KindKey && identity.KeyID != "" {
		namespace = "key:" + identity.KeyID
	}
	sum := sha256.Sum256([]byte(namespace + "\x00" + kind + "\x00" + raw))
	return hex.EncodeToString(sum[:])
}
