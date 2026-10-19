package qoder

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// A stuck upstream that never sends the response head must surface as
// ErrFirstByteTimeout so the executor can fail the attempt over instead of
// hanging the client until it gives up. The production deadline is shrunk for
// the test so the assertion runs in milliseconds.
func TestDoWithFirstByteTimeoutHangsAsFirstByteTimeout(t *testing.T) {
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
	_, err = doWithFirstByteTimeout(client, req, 50*time.Millisecond)
	if !errors.Is(err, providers.ErrFirstByteTimeout) {
		t.Fatalf("want ErrFirstByteTimeout, got %v", err)
	}
}

// When the parent context is already cancelled the helper must propagate the
// plain transport error instead of masking it as a first-byte timeout; the
// executor classifies client cancels as KindCanceled (no failover, no cooldown).
func TestDoWithFirstByteTimeoutParentCancelIsNotFirstByteTimeout(t *testing.T) {
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
	_, err = doWithFirstByteTimeout(client, req, 50*time.Millisecond)
	if errors.Is(err, providers.ErrFirstByteTimeout) {
		t.Fatalf("parent-cancelled call must not surface as ErrFirstByteTimeout, got %v", err)
	}
	if err == nil {
		t.Fatalf("want non-nil error for cancelled parent context")
	}
}

// A well-behaved upstream that returns a response head quickly must not be
// cut off by the TTFB timer, and the whole multi-chunk body must stay readable
// to EOF afterwards. This drains every chunk rather than doing a single buffered
// Read: a cancel-on-return bug only shows up once the stream continues past the
// first read, which is exactly the "dies right after headers" symptom.
func TestDoWithFirstByteTimeoutStreamsBodyToEOF(t *testing.T) {
	const chunks = 4
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < chunks; i++ {
			_, _ = w.Write([]byte("data: chunk\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			// Space the chunks out so the first-byte deadline would fire if the
			// timer were not stopped and the context were cancelled on return.
			time.Sleep(20 * time.Millisecond)
		}
	}))
	t.Cleanup(ok.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ok.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	client := &http.Client{Timeout: 0}
	// Short TTFB deadline: the head arrives immediately, so a correct
	// implementation stops the timer and never cancels the body.
	resp, err := doWithFirstByteTimeout(client, req, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("want nil err for fast head, got %v", err)
	}
	defer resp.Body.Close()

	var got bytes.Buffer
	if _, err := io.Copy(&got, resp.Body); err != nil {
		t.Fatalf("drain body to EOF: %v (read %q)", err, got.String())
	}
	want := strings.Repeat("data: chunk\n\n", chunks)
	if got.String() != want {
		t.Fatalf("body = %q, want %q", got.String(), want)
	}
}
