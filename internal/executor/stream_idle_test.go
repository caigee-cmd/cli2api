package executor

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// silentBody blocks every Read until Close is called, mimicking an upstream
// that sent the response head and then stopped writing bytes.
type silentBody struct {
	ch   chan struct{}
	once sync.Once
}

func newSilentBody() *silentBody { return &silentBody{ch: make(chan struct{})} }

func (b *silentBody) Read(p []byte) (int, error) {
	<-b.ch
	return 0, errors.New("body closed")
}

func (b *silentBody) Close() error {
	b.once.Do(func() { close(b.ch) })
	return nil
}

func TestStreamIdleGuardTripsOnSilentBody(t *testing.T) {
	body := newSilentBody()
	guarded := guardStreamIdle(body, 80*time.Millisecond)
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		_, err := guarded.Read(buf)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrStreamIdle) {
			t.Fatalf("Read err = %v, want ErrStreamIdle", err)
		}
		if !strings.Contains(err.Error(), "80ms") {
			t.Fatalf("err message lost the deadline: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle guard did not unblock a silent read")
	}
	if err := guarded.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestStreamIdleGuardIgnoresConsumerStall(t *testing.T) {
	body := newSilentBody()
	guarded := guardStreamIdle(body, 60*time.Millisecond)
	// No Read is pending: the relay is busy writing to a slow client, which is
	// not the upstream's fault. The guard must not close the body behind it.
	time.Sleep(200 * time.Millisecond)
	if err := guarded.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	buf := make([]byte, 8)
	_, err := guarded.Read(buf)
	if errors.Is(err, ErrStreamIdle) {
		t.Fatalf("idle guard fired without a pending read: %v", err)
	}
}

func TestStreamIdleGuardKeepsActiveStreams(t *testing.T) {
	reader, writer := io.Pipe()
	go func() {
		for i := 0; i < 6; i++ {
			time.Sleep(30 * time.Millisecond)
			if _, err := writer.Write([]byte("x")); err != nil {
				return
			}
		}
		writer.Close()
	}()
	guarded := guardStreamIdle(reader, 100*time.Millisecond)
	data, err := io.ReadAll(guarded)
	if err != nil {
		t.Fatalf("active stream tripped the idle guard: %v", err)
	}
	if len(data) != 6 {
		t.Fatalf("read %d bytes, want 6", len(data))
	}
	if err := guarded.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestAttachInFlightReleaseGuardsIdleAndReleasesSlot(t *testing.T) {
	pool := NewPool([]string{"http://a:3020"}, []string{"a"})
	if !pool.Acquire("a") {
		t.Fatal("acquire")
	}
	body := newSilentBody()
	resp := &http.Response{StatusCode: http.StatusOK, Body: body}
	e := ChatExecutor{Pool: pool}
	got := e.attachInFlightRelease(resp, "req-1", "a")

	closer, ok := got.Body.(*releaseCloser)
	if !ok {
		t.Fatalf("body = %T, want *releaseCloser", got.Body)
	}
	if _, ok := closer.ReadCloser.(*idleGuardBody); !ok {
		t.Fatalf("inner body = %T, want *idleGuardBody", closer.ReadCloser)
	}
	if err := got.Body.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	item, ok := pool.ByID("a")
	if !ok {
		t.Fatal("account missing from pool")
	}
	if item.InFlight != 0 {
		t.Fatalf("slot not released after Close: in_flight=%d", item.InFlight)
	}
}

func TestClassifyStreamIdleIsUpstreamHang(t *testing.T) {
	classified := ClassifyError(errors.Join(ErrStreamIdle, errors.New("no bytes for 5m0s")))
	if classified.Kind != accounts.KindUnavailable {
		t.Fatalf("kind = %q, want unavailable", classified.Kind)
	}
	if classified.Code != "stream_idle_timeout" {
		t.Fatalf("code = %q, want stream_idle_timeout", classified.Code)
	}
	if classified.Failover {
		t.Fatal("idle trips after bytes are on the wire; must not claim a failover hop")
	}
}

func TestResolveStreamIdleTimeout(t *testing.T) {
	t.Setenv(idleStreamEnv, "")
	if got := resolveStreamIdleTimeout(); got != idleStreamDefault {
		t.Fatalf("default = %s, want %s", got, idleStreamDefault)
	}
	t.Setenv(idleStreamEnv, "90")
	if got := resolveStreamIdleTimeout(); got != 30*time.Second {
		t.Fatalf("sub-30s value = %s, want clamp to 30s", got)
	}
	t.Setenv(idleStreamEnv, "240000")
	if got := resolveStreamIdleTimeout(); got != 4*time.Minute {
		t.Fatalf("value = %s, want 4m", got)
	}
	t.Setenv(idleStreamEnv, "bogus")
	if got := resolveStreamIdleTimeout(); got != idleStreamDefault {
		t.Fatalf("invalid value = %s, want default", got)
	}
}
