package workbuddy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// Streaming first-byte timeout.
//
// WorkBuddy's CN gateway sometimes accepts a chat request and then sends nothing
// at all for minutes — an upstream hang, not a rejection. Without a deadline the
// relay waits until the client gives up, the turn is spent, and a retry lands on
// the same account. Qoder already fails over on this (QODER_STREAM_TTFB_MS); this
// is the WorkBuddy counterpart.
const defaultFirstByteTimeout = 60 * time.Second

// workbuddyStreamTTFBEnv overrides the first-byte deadline.
const workbuddyStreamTTFBEnv = "WORKBUDDY_STREAM_TTFB_MS"

// resolveFirstByteTimeout reads WORKBUDDY_STREAM_TTFB_MS. Absent or invalid
// values fall back to the default; sub-5s values clamp up so the knob cannot be
// set so low that normal upstream latency looks like a hang.
func resolveFirstByteTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(workbuddyStreamTTFBEnv))
	if raw == "" {
		return defaultFirstByteTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return defaultFirstByteTimeout
	}
	d := time.Duration(ms) * time.Millisecond
	if d < 5*time.Second {
		return 5 * time.Second
	}
	return d
}

// doWithFirstByteTimeout sends req and returns once the response head arrives.
// If the head does not arrive within timeout the error wraps
// providers.ErrFirstByteTimeout, which the executor classifies as an upstream
// failure and fails over to another account.
//
// The deadline context must NOT be cancelled after a successful head: Go ties
// the response body to the request context, so cancelling would truncate the
// stream that has already started. Only the error path cancels; on success the
// timer stops and the derived context is released by Body.Close.
func doWithFirstByteTimeout(client *http.Client, req *http.Request, timeout time.Duration) (*http.Response, error) {
	if timeout <= 0 {
		timeout = defaultFirstByteTimeout
	}
	parent := req.Context()
	ttfbCtx, cancelTTFB := context.WithCancel(parent)
	timer := time.AfterFunc(timeout, cancelTTFB)
	resp, err := client.Do(req.WithContext(ttfbCtx))
	if err == nil {
		timer.Stop()
		resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancelTTFB}
		return resp, nil
	}
	timer.Stop()
	// Decide the cause before cancelTTFB, which would otherwise make
	// ttfbCtx.Err() non-nil regardless of what actually happened. A parent
	// cancel stays a plain cancel so a client disconnect is not cooled down.
	timedOut := parent.Err() == nil && ttfbCtx.Err() != nil && !errors.Is(parent.Err(), context.Canceled)
	cancelTTFB()
	if timedOut {
		return nil, fmt.Errorf("%w: no response head within %s", providers.ErrFirstByteTimeout, timeout)
	}
	return nil, err
}

// cancelOnCloseBody releases the TTFB-derived context when the caller is done
// with the stream, without cutting the body short the way an eager cancel would.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
