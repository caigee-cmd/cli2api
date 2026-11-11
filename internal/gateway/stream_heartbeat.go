package gateway

import (
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SSE heartbeat while waiting for the upstream response head.
//
// A streaming request normally goes silent between "client finished sending the
// body" and "upstream sent its first SSE frame". Two independent timers watch
// that gap and both can kill a healthy turn:
//
//   - Edge proxies with a fixed response budget abort the connection when the
//     origin has not produced a complete response in time. Cloudflare's 120s
//     Proxy Read Timeout is not configurable outside Enterprise, so waiting
//     for the upstream response head (or a long prefill) can exceed the whole
//     budget while the upstream is working normally.
//   - Clients time out an idle connection for the same reason.
//
// Committing the SSE headers early and emitting comment frames (`: keepalive`)
// turns that silence into observable traffic. Comment frames are ignored by
// every SSE parser, so a spec-following client cannot tell the difference.
//
// The cost: committing the headers forfeits the chance to answer with a real
// HTTP status. So the heartbeat arms a timer and only commits after `after`
// (default 20s). Fast failures (no routable account, quota, auth) still return
// their proper status code; only a genuinely slow attempt degrades to an
// in-stream error event, which every OpenAI-compatible client already handles.

const (
	// defaultHeartbeatAfter keeps fast failures on the normal HTTP-status path.
	defaultHeartbeatAfter = 20 * time.Second
	// defaultHeartbeatInterval stays well under a typical proxy idle window.
	defaultHeartbeatInterval = 10 * time.Second
	// heartbeatEnvAfter / heartbeatEnvInterval tune the timing without a rebuild.
	heartbeatEnvAfter    = "CLI2API_STREAM_HEARTBEAT_AFTER_MS"
	heartbeatEnvInterval = "CLI2API_STREAM_HEARTBEAT_INTERVAL_MS"
)

func resolveHeartbeatAfter() time.Duration {
	return resolveMillisEnv(heartbeatEnvAfter, defaultHeartbeatAfter, time.Second)
}

func resolveHeartbeatInterval() time.Duration {
	return resolveMillisEnv(heartbeatEnvInterval, defaultHeartbeatInterval, time.Second)
}

func resolveMillisEnv(name string, fallback, floor time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return fallback
	}
	d := time.Duration(ms) * time.Millisecond
	if d < floor {
		return floor
	}
	return d
}

// streamHeartbeat owns the early-commit decision and the comment-frame pump.
//
// Three goroutines can touch the ResponseWriter over the life of a request: the
// handler itself, the heartbeat timer, and the pump. net/http does not make
// ResponseWriter safe for concurrent use, so every write goes through writeMu:
//
//   - the pump writes comment frames under writeMu;
//   - handoff closes stop and then waits for pumpDone, so it returns only after
//     the in-flight heartbeat write finished and no further one can start;
//   - after handoff the handler owns the writer exclusively, and the pump has
//     provably stopped.
//
// A nil *streamHeartbeat is a valid no-op value, which is what a writer that
// cannot flush gets.
type streamHeartbeat struct {
	w        http.ResponseWriter
	flusher  http.Flusher
	after    time.Duration
	interval time.Duration

	// writeMu serialises writes to w between the timer's commit and the pump.
	writeMu sync.Mutex

	mu        sync.Mutex
	state     heartbeatState
	timer     *time.Timer
	stop      chan struct{}
	pumpDone  chan struct{}
	committed chan struct{}
	writeErr  error
}

type heartbeatState int

const (
	heartbeatIdle heartbeatState = iota
	heartbeatCommitting
	heartbeatCommitted
	heartbeatAdopted
	heartbeatAbandoned
)

// newStreamHeartbeat returns nil when the writer cannot flush, which leaves the
// original behaviour untouched.
func newStreamHeartbeat(w http.ResponseWriter) *streamHeartbeat {
	return newStreamHeartbeatWith(w, resolveHeartbeatAfter(), resolveHeartbeatInterval())
}

// newStreamHeartbeatWith is the test seam: explicit timings instead of the
// environment, so tests do not have to fight the 1s floor above.
func newStreamHeartbeatWith(w http.ResponseWriter, after, interval time.Duration) *streamHeartbeat {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil
	}
	if after <= 0 {
		after = defaultHeartbeatAfter
	}
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	return &streamHeartbeat{
		w:         w,
		flusher:   flusher,
		after:     after,
		interval:  interval,
		committed: make(chan struct{}),
	}
}

// Arm starts the early-commit timer. setHeaders runs just before WriteHeader so
// the committed response still carries the SSE content type and cache
// directives.
func (h *streamHeartbeat) Arm(setHeaders func()) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.state != heartbeatIdle {
		h.mu.Unlock()
		return
	}
	h.timer = time.AfterFunc(h.after, func() {
		h.mu.Lock()
		if h.state != heartbeatIdle {
			h.mu.Unlock()
			return
		}
		h.state = heartbeatCommitting
		h.stop = make(chan struct{})
		h.pumpDone = make(chan struct{})
		h.mu.Unlock()

		h.writeMu.Lock()
		setHeaders()
		h.w.WriteHeader(http.StatusOK)
		h.flusher.Flush()
		h.writeMu.Unlock()

		h.mu.Lock()
		h.state = heartbeatCommitted
		h.mu.Unlock()
		close(h.committed)

		h.pump()
		close(h.pumpDone)
	})
	h.mu.Unlock()
}

// Adopt hands the response to the real relay. It reports whether the timer had
// already committed the headers, in which case the caller must not set headers
// or call WriteHeader again.
func (h *streamHeartbeat) Adopt() bool {
	return h.handoff(heartbeatAdopted)
}

// Abandon retires the heartbeat on the error path and reports whether the
// headers were already committed. When they were, the failure can only be
// reported as an in-stream error event.
func (h *streamHeartbeat) Abandon() bool {
	return h.handoff(heartbeatAbandoned)
}

// handoff retires the heartbeat. When the headers were committed it blocks
// until the pump has exited, so the caller owns the writer exclusively.
func (h *streamHeartbeat) handoff(next heartbeatState) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	state := h.state
	stop := h.stop
	committed := h.committed
	pumpDone := h.pumpDone
	h.mu.Unlock()

	switch state {
	case heartbeatIdle:
		// The timer never fired. Flip the state first: a late callback
		// re-checks it under the same mutex and bails out before writing.
		h.mu.Lock()
		if h.state == heartbeatIdle {
			h.state = next
		}
		h.mu.Unlock()
		if h.timer != nil {
			h.timer.Stop()
		}
		return false
	case heartbeatCommitting, heartbeatCommitted:
		if stop != nil {
			select {
			case <-stop:
			default:
				close(stop)
			}
		}
		// committed is closed once the headers are on the wire; pumpDone once
		// the pump has stopped writing. Waiting on pumpDone is what makes the
		// handoff race-free. Both are created by the timer callback before it
		// publishes heartbeatCommitting, so they are non-nil whenever this
		// branch runs.
		if committed != nil {
			<-committed
		}
		if pumpDone != nil {
			<-pumpDone
		}
		h.mu.Lock()
		h.state = next
		h.mu.Unlock()
		if h.timer != nil {
			h.timer.Stop()
		}
		return true
	default:
		// Already adopted or abandoned: the timer never committed headers.
		return false
	}
}

// pump emits comment frames until handoff closes stop.
func (h *streamHeartbeat) pump() {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-ticker.C:
			h.writeMu.Lock()
			_, err := io.WriteString(h.w, ": keepalive\n\n")
			if err == nil {
				h.flusher.Flush()
			}
			h.writeMu.Unlock()
			if err != nil {
				h.mu.Lock()
				if h.writeErr == nil {
					h.writeErr = err
				}
				h.mu.Unlock()
				return
			}
		}
	}
}

// clientGone reports a heartbeat write failure so the handler can record the
// request as a client disconnect instead of an upstream failure.
func (h *streamHeartbeat) clientGone() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.writeErr
}
