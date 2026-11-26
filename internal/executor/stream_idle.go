package executor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrStreamIdle reports that an upstream answered the response head and then
// stopped sending bytes for longer than the idle deadline. It is deliberately
// distinct from a client cancel: the caller is still there and the upstream is
// the one that went silent.
var ErrStreamIdle = errors.New("upstream stream idle timeout")

// idleStreamDefault is how long a streaming response may stay silent before the
// gateway gives up on it.
//
// Without this bound an upstream that accepts a request, sends the head plus a
// token or two, and then stalls keeps the pool slot, the client connection and
// the request log open forever. The client-side heartbeat hides the stall from
// the caller, so nothing ever gives up: the console keeps showing an extra
// in-flight slot until the container restarts. qauth already bounds Qoder PAT
// streams this way; in-process adapters like WorkBuddy stream with a zero
// http.Client timeout, so the executor has to own the deadline.
const idleStreamDefault = 5 * time.Minute

// idleStreamEnv overrides the idle deadline in milliseconds.
const idleStreamEnv = "CLI2API_STREAM_IDLE_MS"

// resolveStreamIdleTimeout reads CLI2API_STREAM_IDLE_MS. Absent, invalid or
// sub-30s values fall back to the default (and clamp up): a shorter deadline
// would cut legitimate slow prefills.
func resolveStreamIdleTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(idleStreamEnv))
	if raw == "" {
		return idleStreamDefault
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return idleStreamDefault
	}
	d := time.Duration(ms) * time.Millisecond
	if d < 30*time.Second {
		return 30 * time.Second
	}
	return d
}

// idleGuardBody closes the wrapped body once a read has been blocked longer
// than the deadline. Upstream bodies cannot be given a read deadline in
// general, but Close unblocks a pending Read, which is how the relay learns the
// stream is dead instead of waiting forever.
type idleGuardBody struct {
	inner   io.ReadCloser
	timeout time.Duration
	last    atomic.Int64 // UnixNano of the last byte received, or of the current blocked Read
	pending atomic.Int32 // number of Reads currently blocked on the inner body
	tripped atomic.Bool
	done    chan struct{}
	once    sync.Once
}

func guardStreamIdle(body io.ReadCloser, timeout time.Duration) io.ReadCloser {
	if body == nil {
		return body
	}
	if timeout <= 0 {
		timeout = idleStreamDefault
	}
	b := &idleGuardBody{inner: body, timeout: timeout, done: make(chan struct{})}
	b.last.Store(time.Now().UnixNano())
	go b.watch()
	return b
}

func (b *idleGuardBody) watch() {
	every := b.timeout / 4
	if every < 50*time.Millisecond {
		every = 50 * time.Millisecond
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			// Only a blocked Read can be idle-worthy. While the relay is writing
			// to a slow client no Read is pending, and the upstream is not at
			// fault for that wait.
			if b.pending.Load() == 0 {
				continue
			}
			if time.Since(time.Unix(0, b.last.Load())) >= b.timeout {
				b.tripped.Store(true)
				// Unblock the in-flight Read. The error it returns is mapped back
				// to ErrStreamIdle so the relay reports an upstream timeout rather
				// than a truncated body.
				b.inner.Close()
				return
			}
		}
	}
}

func (b *idleGuardBody) Read(p []byte) (int, error) {
	if b.pending.Add(1) == 1 {
		// Start the idle clock when the read actually starts waiting; bytes that
		// were consumed earlier belong to the previous read.
		b.last.Store(time.Now().UnixNano())
	}
	n, err := b.inner.Read(p)
	b.pending.Add(-1)
	if n > 0 {
		b.last.Store(time.Now().UnixNano())
	}
	if err != nil && b.tripped.Load() {
		return n, fmt.Errorf("%w: no bytes for %s", ErrStreamIdle, b.timeout)
	}
	return n, err
}

func (b *idleGuardBody) Close() error {
	b.once.Do(func() { close(b.done) })
	return b.inner.Close()
}
