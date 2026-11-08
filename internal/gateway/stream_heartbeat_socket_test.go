package gateway

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This is the end-to-end proof over a real TCP socket: the handler blocks on a
// slow "upstream", and a real net/http client must observe bytes on the wire
// long before the body arrives. That is exactly what keeps an intermediary with
// a fixed response budget from aborting the request.
func TestHeartbeatOverRealSocketDeliversBytesBeforeBody(t *testing.T) {
	bodyDelay := 700 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		heartbeat := newStreamHeartbeatWith(w, 80*time.Millisecond, 50*time.Millisecond)
		heartbeat.Arm(func() {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
		})

		// Stand-in for "blocked inside ChatStreamProxy waiting for the upstream
		// response head".
		time.Sleep(bodyDelay)

		committed := heartbeat.Adopt()
		if !committed {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.WriteHeader(http.StatusOK)
		}
		if _, err := w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
			return
		}
	}))
	defer srv.Close()

	started := time.Now()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("Content-Type = %q", got)
	}

	reader := bufio.NewReader(resp.Body)
	firstByteAt := time.Duration(0)
	var sawKeepalive bool
	var sawBody bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if line != "" && firstByteAt == 0 {
			firstByteAt = time.Since(started)
		}
		if strings.HasPrefix(line, ": keepalive") {
			sawKeepalive = true
		}
		if strings.HasPrefix(line, "data: [DONE]") {
			sawBody = true
			break
		}
		if err != nil {
			break
		}
	}

	if !sawKeepalive {
		t.Fatal("client never saw a heartbeat frame")
	}
	if !sawBody {
		t.Fatal("stream never completed")
	}
	// The first byte must land well before the body: that gap is the window an
	// edge proxy would otherwise count as silence.
	if firstByteAt >= bodyDelay {
		t.Fatalf("first byte at %s, not before the body at %s", firstByteAt, bodyDelay)
	}
	t.Logf("first byte at %s, body at ~%s", firstByteAt, bodyDelay)
}
