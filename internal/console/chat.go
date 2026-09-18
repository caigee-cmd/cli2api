package console

import "net/http"

func (h *Handler) HandleChat(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.Chat == nil {
		writeErr(w, http.StatusServiceUnavailable, "chat_unavailable", "chat handler unavailable")
		return
	}
	h.Chat(w, r)
}
