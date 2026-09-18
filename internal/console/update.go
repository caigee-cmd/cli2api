package console

import "net/http"

func (h *Handler) HandleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Update == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	h.Update.Handle(w, r)
}

func (h *Handler) HandleSystemUpdatePrepare(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Update == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	h.Update.HandlePrepare(w, r)
}

func (h *Handler) HandleSystemUpdateConfirm(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Update == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	h.Update.HandleApply(w, r)
}

func (h *Handler) HandleSystemUpdateCancel(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Update == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	h.Update.HandleCancel(w, r)
}

func (h *Handler) HandleSystemUpdateRollback(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Update == nil {
		writeErr(w, http.StatusServiceUnavailable, "update_unavailable", "update coordinator unavailable")
		return
	}
	h.Update.HandleRollback(w, r)
}
