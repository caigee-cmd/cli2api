package gateway

import (
	"net/http"
	"sync/atomic"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// CatalogQuery loads the public /v1/models display catalog. Gateway must not
// import store or the runtime manager; api injects the existing fetch path.
type CatalogQuery func(refresh bool, accountID string) ([]map[string]any, error)

type Handler struct {
	Executor          executor.ChatExecutor
	Recorder          *applogs.RequestRecorder
	Pool              *executor.Pool
	ModelContexts     executor.ModelContextStore
	Catalogs          executor.CatalogPreparer
	Logs              executor.RequestStarter
	CrossProviderPool *atomic.Bool
	Models            CatalogQuery
	RequestedAccount  func(*http.Request) string
	FilterModels      func(*http.Request, []map[string]any) []map[string]any
	DecorateModels    func(*http.Request, []map[string]any) []map[string]any
}

func (h *Handler) requestIdentity(r *http.Request) auth.Identity {
	if r == nil {
		return auth.Identity{Kind: auth.KindNone}
	}
	identity, ok := auth.IdentityFrom(r.Context())
	if ok {
		return identity
	}
	return auth.Identity{Kind: auth.KindNone}
}

func (h *Handler) requestedAccount(r *http.Request) string {
	if h != nil && h.RequestedAccount != nil {
		return h.RequestedAccount(r)
	}
	return ""
}

func (h *Handler) crossProviderPoolOn() bool {
	return h != nil && h.CrossProviderPool != nil && h.CrossProviderPool.Load()
}

func (h *Handler) rejectsBareModel(model string) bool {
	return executor.RejectsBareModel(model, h.crossProviderPoolOn())
}

func (h *Handler) resolveProviderFilter(req *translate.ChatRequest) string {
	return executor.ResolveProviderFilter(req, h.crossProviderPoolOn())
}

func (h *Handler) applyPinnedProviderFilter(providerFilter, publicModel, prefer string) string {
	if h == nil {
		return providerFilter
	}
	return executor.ApplyPinnedProviderFilter(h.Pool, providerFilter, publicModel, prefer)
}
