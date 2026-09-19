package gateway

import (
	"net/http"
	"sync/atomic"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
)

// CatalogQuery loads the public /v1/models display catalog. Gateway must not
// import store or the runtime manager; app injects the catalog fetch path.
type CatalogQuery func(refresh bool, accountID string) ([]map[string]any, error)

type Handler struct {
	Executor          executor.ChatExecutor
	Pool              *executor.Pool
	Recorder          *applogs.RequestRecorder
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
