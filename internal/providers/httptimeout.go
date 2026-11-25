// Shared streaming head timeout: an upstream that accepts the request and then
// sends nothing is a hang, not a rejection. Every adapter that streams should
// fail the attempt over with ErrFirstByteTimeout instead of letting the relay
// wait until the client gives up.
package providers

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
)

// DefaultStreamHeadTimeout is the shared ceiling before a silent upstream is
// treated as a hang.
const DefaultStreamHeadTimeout = 60 * time.Second

// HeadTimeoutFromEnv resolves a per-provider knob (milliseconds). Unset or
// invalid values fall back to the default; anything under five seconds clamps up
// so normal upstream latency is never mistaken for a hang.
func HeadTimeoutFromEnv(env string, def time.Duration) time.Duration {
	if def <= 0 {
		def = DefaultStreamHeadTimeout
	}
	raw := strings.TrimSpace(os.Getenv(env))
	if raw == "" {
		return def
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return def
	}
	d := time.Duration(ms) * time.Millisecond
	if d < 5*time.Second {
		return 5 * time.Second
	}
	return d
}

// DoWithHeadTimeout sends req and returns once the response head arrives.
//
// A timeout wraps ErrFirstByteTimeout so the executor can fail the attempt over
// to another account. The deadline context must not be cancelled after a
// successful head — Go ties the body to the request context, so cancelling would
// truncate a stream that already started. Only the error path cancels; on
// success the timer stops and the derived context is released when the body is
// closed.
func DoWithHeadTimeout(client *http.Client, req *http.Request, timeout time.Duration) (*http.Response, error) {
	if timeout <= 0 {
		timeout = DefaultStreamHeadTimeout
	}
	parent := req.Context()
	headCtx, cancelHead := context.WithCancel(parent)
	timer := time.AfterFunc(timeout, cancelHead)
	resp, err := client.Do(req.WithContext(headCtx))
	if err == nil {
		timer.Stop()
		resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancelHead}
		return resp, nil
	}
	timer.Stop()
	// Decide the cause before cancelHead, which would otherwise make
	// headCtx.Err() non-nil regardless of what actually happened. A parent cancel
	// stays a plain cancel so a client disconnect is not cooled down.
	timedOut := parent.Err() == nil && headCtx.Err() != nil && !errors.Is(parent.Err(), context.Canceled)
	cancelHead()
	if timedOut {
		return nil, fmt.Errorf("%w: no response head within %s", ErrFirstByteTimeout, timeout)
	}
	return nil, err
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
