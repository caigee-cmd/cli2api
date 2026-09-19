package console

import (
	"net/http"

	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

func (h *Handler) updateCoordinator() *appupdate.Coordinator {
	if h != nil {
		return h.Update
	}
	return nil
}

func (h *Handler) HandleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	coord := h.updateCoordinator()
	if coord == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	coord.Handle(w, r)
}

func (h *Handler) HandleSystemUpdatePrepare(w http.ResponseWriter, r *http.Request) {
	coord := h.updateCoordinator()
	if coord == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	coord.HandlePrepare(w, r)
}

func (h *Handler) HandleSystemUpdateConfirm(w http.ResponseWriter, r *http.Request) {
	coord := h.updateCoordinator()
	if coord == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	coord.HandleApply(w, r)
}

func (h *Handler) HandleSystemUpdateCancel(w http.ResponseWriter, r *http.Request) {
	coord := h.updateCoordinator()
	if coord == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	coord.HandleCancel(w, r)
}

func (h *Handler) HandleSystemUpdateRollback(w http.ResponseWriter, r *http.Request) {
	coord := h.updateCoordinator()
	if coord == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	coord.HandleRollback(w, r)
}
