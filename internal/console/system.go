package console

import (
	"encoding/json"
	"github.com/caigee-cmd/cli2api/internal/control"
	"net/http"
)

func (h *Handler) HandleSystemSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, h.System.Current(r.Context()))
	case http.MethodPatch:
		var input control.SystemSettingsPatch
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if err := h.System.Patch(r.Context(), input); err != nil {
			writeOperationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, h.System.Current(r.Context()))
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or PATCH only")
	}
}
