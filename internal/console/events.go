package console

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// HandleEvents streams real backend state changes (quota snapshots, check-in
// outcomes, health/cooldown merges, bulk-op progress) as Server-Sent Events.
// Every frame reflects an actual backend mutation; nothing is synthesized.
// Reconnects send Last-Event-ID (or ?last_seq=) and the bus replays its ring.
func (h *Handler) HandleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming_unsupported", "response writer cannot stream")
		return
	}
	var lastSeq uint64
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		lastSeq, _ = strconv.ParseUint(raw, 10, 64)
	} else if raw := r.URL.Query().Get("last_seq"); raw != "" {
		lastSeq, _ = strconv.ParseUint(raw, 10, 64)
	}
	stream, cancel := h.Control.SubscribeEvents(lastSeq)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-stream:
			if !ok {
				// The bus closed a slow consumer. The client reconnects with
				// Last-Event-ID and replays missed events from the ring.
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Type, payload)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}
