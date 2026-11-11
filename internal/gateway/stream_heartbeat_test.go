package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingWriter is an http.ResponseWriter that keeps a timeline of writes so
// tests can assert that heartbeat frames arrived before the body did.
type recordingWriter struct {
	mu       sync.Mutex
	header   http.Header
	status   int
	body     strings.Builder
	writeAt  []time.Duration
	start    time.Time
	flushes  int
	writeErr error
}

func newRecordingWriter() *recordingWriter {
	return &recordingWriter{header: http.Header{}, start: time.Now()}
}

func (w *recordingWriter) Header() http.Header { return w.header }

func (w *recordingWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = status
	}
}

func (w *recordingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.body.Write(p)
	w.writeAt = append(w.writeAt, time.Since(w.start))
	return len(p), nil
}

func (w *recordingWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushes++
}

func (w *recordingWriter) snapshot() (status int, body string, writeAt []time.Duration, flushes int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.status, w.body.String(), append([]time.Duration(nil), w.writeAt...), w.flushes
}

func headerFunc(w http.ResponseWriter) func() {
	return func() {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
	}
}

// The timer must commit SSE headers and start emitting comment frames while the
// handler is still blocked on the upstream head.
func TestHeartbeatCommitsHeadersWhileUpstreamHeadIsPending(t *testing.T) {
	w := newRecordingWriter()
	h := newStreamHeartbeatWith(w, 50*time.Millisecond, 20*time.Millisecond)
	if h == nil {
		t.Fatal("heartbeat disabled for a flushing writer")
	}
	h.Arm(headerFunc(w))

	// Simulate the handler blocked on the upstream.
	time.Sleep(150 * time.Millisecond)

	if committed := h.Adopt(); !committed {
		t.Fatal("Adopt reported no commit, but the timer should have fired by now")
	}
	status, body, _, flushes := w.snapshot()
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := w.header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	if !strings.Contains(body, ": keepalive") {
		t.Fatalf("body has no heartbeat frame: %q", body)
	}
	if flushes == 0 {
		t.Fatal("no flush after the committing write")
	}
}

// A fast upstream must keep the plain HTTP-status path: the timer is disarmed,
// nothing is written, and the caller still owns the response.
func TestHeartbeatDoesNotCommitBeforeDeadline(t *testing.T) {
	w := newRecordingWriter()
	h := newStreamHeartbeatWith(w, 5*time.Second, time.Second)
	h.Arm(headerFunc(w))

	if committed := h.Adopt(); committed {
		t.Fatal("Adopt reported a commit, but the timer had not fired")
	}
	status, body, _, _ := w.snapshot()
	if status != 0 || body != "" {
		t.Fatalf("writer touched before the deadline: status=%d body=%q", status, body)
	}

	// Prove the timer really was disarmed rather than merely late.
	time.Sleep(80 * time.Millisecond)
	if status, _, _, _ := w.snapshot(); status != 0 {
		t.Fatalf("timer wrote headers after Adopt, status=%d", status)
	}
}

// The error path needs to know whether it can still answer with a status code.
func TestHeartbeatAbandonReportsCommit(t *testing.T) {
	w := newRecordingWriter()
	h := newStreamHeartbeatWith(w, 50*time.Millisecond, 20*time.Millisecond)
	h.Arm(headerFunc(w))
	time.Sleep(150 * time.Millisecond)

	if committed := h.Abandon(); !committed {
		t.Fatal("Abandon reported no commit after the timer fired")
	}
	if status, _, _, _ := w.snapshot(); status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
}

// Same path, but the upstream fails fast: the handler keeps its status code.
func TestHeartbeatAbandonBeforeDeadlineKeepsStatusPath(t *testing.T) {
	w := newRecordingWriter()
	h := newStreamHeartbeatWith(w, 5*time.Second, time.Second)
	h.Arm(headerFunc(w))

	if committed := h.Abandon(); committed {
		t.Fatal("Abandon reported a commit before the deadline")
	}
	if status, body, _, _ := w.snapshot(); status != 0 || body != "" {
		t.Fatalf("writer touched: status=%d body=%q", status, body)
	}
}

// A writer without http.Flusher cannot be heartbeated; everything must no-op so
// the original behaviour is preserved.
func TestHeartbeatDisabledWithoutFlusher(t *testing.T) {
	noFlusher := struct {
		http.ResponseWriter
	}{ResponseWriter: httptest.NewRecorder()}

	h := newStreamHeartbeat(noFlusher)
	if h != nil {
		t.Fatal("heartbeat should be nil for a non-flushing writer")
	}
	// All methods must be nil-safe.
	h.Arm(func() { t.Fatal("setHeaders must not run") })
	if h.Adopt() || h.Abandon() || h.clientGone() != nil {
		t.Fatal("nil heartbeat must be inert")
	}
}

// The pump must stop once the relay takes over, otherwise it would keep
// writing comment frames into a response body that already has real chunks.
func TestHeartbeatPumpStopsAfterAdopt(t *testing.T) {
	w := newRecordingWriter()
	h := newStreamHeartbeatWith(w, 30*time.Millisecond, 10*time.Millisecond)
	h.Arm(headerFunc(w))
	time.Sleep(90 * time.Millisecond)

	if !h.Adopt() {
		t.Fatal("expected a commit")
	}
	_, _, writesAtAdopt, _ := w.snapshot()
	writesAtAdopt = append([]time.Duration(nil), writesAtAdopt...)

	time.Sleep(120 * time.Millisecond)
	_, _, writesLater, _ := w.snapshot()
	if len(writesLater) > len(writesAtAdopt)+1 {
		t.Fatalf("pump kept writing after Adopt: %d writes at adopt, %d later",
			len(writesAtAdopt), len(writesLater))
	}
}

// A heartbeat write failure means the client is gone; record it instead of
// spinning.
func TestHeartbeatRecordsClientWriteError(t *testing.T) {
	w := newRecordingWriter()
	w.writeErr = errWriteClosed
	h := newStreamHeartbeatWith(w, 30*time.Millisecond, 10*time.Millisecond)
	h.Arm(headerFunc(w))
	time.Sleep(90 * time.Millisecond)

	if h.Adopt() != true {
		t.Fatal("expected the headers to be committed")
	}
	if got := h.clientGone(); got == nil {
		t.Fatal("clientGone() = nil after a failed heartbeat write")
	}
}

var errWriteClosed = &StreamRelayWriteError{err: http.ErrHandlerTimeout}

// Identity headers registered in setHeaders must already be on the writer when
// the timer commits, because WriteHeader is the last chance to send them.
func TestHeartbeatCommitsAccountProviderHeaders(t *testing.T) {
	w := newRecordingWriter()
	h := newStreamHeartbeatWith(w, 40*time.Millisecond, 20*time.Millisecond)
	h.Arm(func() {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("X-Qoder-Account", "acct-1")
		w.Header().Set("X-CLI2API-Account", "acct-1")
		w.Header().Set("X-CLI2API-Provider", "qoder")
	})
	time.Sleep(120 * time.Millisecond)
	if !h.Adopt() {
		t.Fatal("expected the headers to be committed")
	}
	if got := w.header.Get("X-CLI2API-Account"); got != "acct-1" {
		t.Fatalf("X-CLI2API-Account = %q, want acct-1", got)
	}
	if got := w.header.Get("X-Qoder-Account"); got != "acct-1" {
		t.Fatalf("X-Qoder-Account = %q, want acct-1", got)
	}
	if got := w.header.Get("X-CLI2API-Provider"); got != "qoder" {
		t.Fatalf("X-CLI2API-Provider = %q, want qoder", got)
	}
}
