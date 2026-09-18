package console

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/caigee-cmd/cli2api/internal/config"
	appsvc "github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/executor"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
	"github.com/caigee-cmd/cli2api/internal/providers"
	control "github.com/caigee-cmd/cli2api/internal/update"
)

// Handler is the operator HTTP surface. It decodes requests, calls control/
// logs/update services, and maps responses. It must not import store or the
// runtime manager; catalog fetch, Qoder worker proxy, and live auth rotation
// are injected by app.
type Handler struct {
	Control           *appsvc.Services
	Cfg               *config.Config
	Executor          *executor.ChatExecutor
	Pool              *executor.Pool
	Providers         *providers.Registry
	Recorder          *applogs.RequestRecorder
	Ring              *applogs.Ring
	CrossProviderPool *atomic.Bool
	SettingsMu        *sync.Mutex
	Update            *control.Coordinator

	Chat                http.HandlerFunc
	RequestedAccount    func(*http.Request) string
	FilterModels        func(*http.Request, []map[string]any) []map[string]any
	DecorateModels      func(context.Context, []map[string]any) []map[string]any
	FetchWorkerModels   func(refresh bool) []map[string]any
	FetchDisplayModels  func(refresh bool, accountID string, mode appsvc.CatalogMode) ([]map[string]any, error)
	ProxyAccountWorker  func(http.ResponseWriter, *http.Request, string, string, string)
	GenerateAPIKey      func() (string, error)
	OnConsoleKeyRotated func(secret string)
	ConsoleKey          func() string

	statsCacheMu sync.Mutex
	statsCache   map[string]statsCacheEntry
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

func (h *Handler) filterModels(r *http.Request, models []map[string]any) []map[string]any {
	if h != nil && h.FilterModels != nil {
		return h.FilterModels(r, models)
	}
	return models
}

func (h *Handler) decorateModels(ctx context.Context, models []map[string]any) []map[string]any {
	if h != nil && h.DecorateModels != nil {
		return h.DecorateModels(ctx, models)
	}
	return models
}

func (h *Handler) fetchWorkerModels(refresh bool) []map[string]any {
	if h != nil && h.FetchWorkerModels != nil {
		return h.FetchWorkerModels(refresh)
	}
	return nil
}

func (h *Handler) fetchDisplayModels(refresh bool, accountID string, mode appsvc.CatalogMode) ([]map[string]any, error) {
	if h != nil && h.FetchDisplayModels != nil {
		return h.FetchDisplayModels(refresh, accountID, mode)
	}
	return nil, nil
}

func (h *Handler) proxyAccountWorker(w http.ResponseWriter, r *http.Request, accountID, path, syncAuth string) {
	if h != nil && h.ProxyAccountWorker != nil {
		h.ProxyAccountWorker(w, r, accountID, path, syncAuth)
	}
}

func (h *Handler) cfgPort() int {
	if h == nil || h.Cfg == nil {
		return 0
	}
	return h.Cfg.Port
}

func (h *Handler) cfgProxyAPIKey() string {
	if h != nil && h.ConsoleKey != nil {
		return h.ConsoleKey()
	}
	if h == nil || h.Cfg == nil {
		return ""
	}
	return h.Cfg.ProxyAPIKey
}

func (h *Handler) StatsCacheSize() int {
	if h == nil {
		return 0
	}
	h.statsCacheMu.Lock()
	defer h.statsCacheMu.Unlock()
	return len(h.statsCache)
}
