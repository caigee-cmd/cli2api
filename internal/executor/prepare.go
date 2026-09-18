package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type PrepareError struct {
	Status  int
	Code    string
	Message string
}

func (e *PrepareError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type ModelContextStore interface {
	GetModelContext(ctx context.Context, modelID string) (int, bool, error)
}

type CatalogPreparer interface {
	EnsureModelCatalogs(ctx context.Context, force bool)
}

type RequestStarter interface {
	Start(entry accounts.RequestLog)
}

type PrepareInput struct {
	Context           context.Context
	Request           translate.ChatRequest
	Identity          auth.Identity
	PreferAccount     string
	SessionHeader     string
	CrossProviderPool bool
	Now               time.Time
	NewRequestID      func() string
	ModelContexts     ModelContextStore
	Catalogs          CatalogPreparer
	Logs              RequestStarter
}

type PreparedRequest struct {
	Context        context.Context
	RequestID      string
	Started        time.Time
	Request        translate.ChatRequest
	PublicModel    string
	ProviderFilter string
	Prefer         string
}

func (e ChatExecutor) Prepare(in PrepareInput) (PreparedRequest, error) {
	if in.Context == nil {
		in.Context = context.Background()
	}
	request := in.Request
	publicModel := request.Model
	if RejectsBareModel(publicModel, in.CrossProviderPool) {
		return PreparedRequest{}, &PrepareError{
			Status:  400,
			Code:    "provider_prefix_required",
			Message: "cross-provider model pool is disabled; use a provider-prefixed model ID such as qoder/glm-5.2",
		}
	}
	providerFilter := ResolveProviderFilter(&request, in.CrossProviderPool)
	prefer := strings.TrimSpace(in.PreferAccount)
	providerFilter = ApplyPinnedProviderFilter(e.Pool, providerFilter, publicModel, prefer)
	if providerFilter != "" && !in.Identity.AllowsProvider(providerFilter) {
		return PreparedRequest{}, &PrepareError{
			Status:  403,
			Code:    "provider_not_allowed",
			Message: "This API key cannot use provider " + providerFilter,
		}
	}
	if err := ApplyModelContextDefaults(in.Context, in.ModelContexts, &request, providerFilter); err != nil {
		return PreparedRequest{}, &PrepareError{Status: 500, Code: "model_setting_failed", Message: err.Error()}
	}
	if in.Catalogs != nil {
		in.Catalogs.EnsureModelCatalogs(in.Context, false)
	}
	newID := in.NewRequestID
	if newID == nil {
		newID = accounts.NewRequestID
	}
	requestID := newID()
	started := in.Now
	if started.IsZero() {
		started = time.Now().UTC()
	} else {
		started = started.UTC()
	}
	if in.Logs != nil {
		in.Logs.Start(accounts.RequestLog{
			ID:                  requestID,
			CreatedAt:           started,
			Stream:              request.Stream,
			Status:              accounts.RequestStatusStarted,
			RequestedModel:      firstNonEmpty(publicModel, request.Model),
			MessageCount:        len(request.Messages),
			EmptyMessageIndexes: translate.EmptyMessageIndexes(request.Messages),
			MessageRoles:        translate.MessageRoles(request.Messages),
		})
	}
	ctx := WithAllowedProviders(WithRequestID(in.Context, requestID), in.Identity.AllowedProviders)
	if sessionKey := SessionKeyFor(in.SessionHeader, in.Identity, request); sessionKey != "" {
		ctx = WithSessionKey(ctx, sessionKey)
	}
	return PreparedRequest{
		Context:        ctx,
		RequestID:      requestID,
		Started:        started,
		Request:        request,
		PublicModel:    publicModel,
		ProviderFilter: providerFilter,
		Prefer:         prefer,
	}, nil
}

func ProviderPrefix(model string) string {
	model = strings.TrimSpace(model)
	for _, prefix := range []string{"qoder/", "workbuddy/", "trae/", "devin/"} {
		if strings.HasPrefix(model, prefix) {
			return strings.TrimSuffix(prefix, "/")
		}
	}
	return ""
}

func RejectsBareModel(model string, crossProviderPool bool) bool {
	return strings.TrimSpace(model) != "" && !crossProviderPool && ProviderPrefix(model) == ""
}

func ResolveProviderFilter(req *translate.ChatRequest, crossProviderPool bool) string {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return ""
	}
	if prefix := ProviderPrefix(model); prefix != "" {
		req.Model = strings.TrimPrefix(model, prefix+"/")
		return prefix
	}
	if crossProviderPool {
		return ""
	}
	return "qoder"
}

func ApplyPinnedProviderFilter(pool *Pool, providerFilter, publicModel, prefer string) string {
	if prefer == "" || pool == nil {
		return providerFilter
	}
	if ProviderPrefix(publicModel) != "" {
		return providerFilter
	}
	item, ok := pool.ByID(prefer)
	if !ok {
		return providerFilter
	}
	pinned := accounts.NormalizeProviderFamily(item.Provider)
	if providerFilter == "" || providerFilter == pinned {
		return providerFilter
	}
	return pinned
}

func ApplyModelContextDefaults(ctx context.Context, store ModelContextStore, req *translate.ChatRequest, providerFilter string) error {
	if store == nil || req == nil || strings.TrimSpace(req.Model) == "" {
		return nil
	}
	if providerFilter != "" && providerFilter != "qoder" {
		return nil
	}
	contextLength, ok, err := store.GetModelContext(ctx, modelContextKey(req.Model))
	if err != nil || !ok {
		return err
	}
	value := json.RawMessage(strconv.Itoa(contextLength))
	if len(req.ContextLength) == 0 {
		req.ContextLength = append(json.RawMessage(nil), value...)
	}
	if len(req.MaxInputTokens) == 0 {
		req.MaxInputTokens = append(json.RawMessage(nil), value...)
	}
	return nil
}

func modelContextKey(model string) string {
	return accounts.CanonicalModelID(model)
}

func SessionKeyFor(header string, identity auth.Identity, req translate.ChatRequest) string {
	raw := strings.TrimSpace(header)
	kind := "content"
	if raw != "" {
		kind = "header"
	} else {
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
