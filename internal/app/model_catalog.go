package app

import (
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"net/http"
)

func (a *App) RequestedAccount(r *http.Request) string { return endpoint.RequestedAccount(r) }
func (a *App) filterModelsForIdentity(r *http.Request, models []map[string]any) []map[string]any {
	return control.FilterModelsForIdentity(a.requestIdentity(r), models)
}
