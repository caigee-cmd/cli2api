package qoder

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// A stuck upstream that never sends the response head must surface as
// ErrFirstByteTimeout so the executor can fail the attempt over instead of
// hanging the client until it gives up. The 60s production deadline is shrunk
// for the test so the assertion runs in milliseconds.
func TestDoWithFirstByteTimeoutHangsAsFirstByteTimeout(t *testing.T) {
	prev := firstByteTimeout
	firstByteTimeout = 50 * time.Millisecond
	t.Cleanup(func() { firstByteTimeout = prev })

	// Handler blocks forever: it accepts the body but never writes a response.
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(hung.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, hung.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	client := &http.Client{Timeout: 0} // mimic streaming client: no whole-call timeout
	_, err = doWithFirstByteTimeout(client, req)
	if !errors.Is(err, providers.ErrFirstByteTimeout) {
		t.Fatalf("want ErrFirstByteTimeout, got %v", err)
	}
}

// When the parent context is already cancelled the helper must propagate the
// plain transport error instead of masking it as a first-byte timeout; the
// executor classifies client cancels as KindCanceled (no failover, no cooldown).
func TestDoWithFirstByteTimeoutParentCancelIsNotFirstByteTimeout(t *testing.T) {
	prev := firstByteTimeout
	firstByteTimeout = 50 * time.Millisecond
	t.Cleanup(func() { firstByteTimeout = prev })

	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(hung.Close)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hung.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	client := &http.Client{Timeout: 0}

	// Cancel before the call so parent.Err() is non-nil at the point of failure.
	cancel()
	_, err = doWithFirstByteTimeout(client, req)
	if errors.Is(err, providers.ErrFirstByteTimeout) {
		t.Fatalf("parent-cancelled call must not surface as ErrFirstByteTimeout, got %v", err)
	}
	if err == nil {
		t.Fatalf("want non-nil error for cancelled parent context")
	}
}

// A well-behaved upstream that returns a response head quickly must not be
// cut off by the TTFB timer, and the body must stay readable afterwards.
func TestDoWithFirstByteTimeoutFastHeadReturnsBody(t *testing.T) {
	prev := firstByteTimeout
	firstByteTimeout = 5 * time.Second
	t.Cleanup(func() { firstByteTimeout = prev })

	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Slow trickle SSE chunks to prove the body is readable past the TTFB
		// timer being stopped.
		flusher, _ := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			_, _ = w.Write([]byte("data: chunk\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(20 * time.Millisecond)
		}
	}))
	t.Cleanup(ok.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ok.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	client := &http.Client{Timeout: 0}
	resp, err := doWithFirstByteTimeout(client, req)
	if err != nil {
		t.Fatalf("want nil err for fast head, got %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 1024)
	n, err := resp.Body.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("body read after TTFB timer stop: n=%d err=%v", n, err)
	}
}
