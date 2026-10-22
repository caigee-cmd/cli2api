package qoder

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestUpstreamStatusErrorQueued10605(t *testing.T) {
	body := []byte(`403 {"code":"10605","message":"{\"isQueued\":true,\"retryAfterSeconds\":7}"}`)
	err := upstreamStatusError(http.StatusForbidden, body)
	var pe *providers.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err=%v", err)
	}
	if pe.Kind != accounts.KindRateLimit || pe.Code != providers.CodeUpstreamQueued || pe.RetryAfter != 7*time.Second {
		t.Fatalf("queued 403 misclassified: %+v", pe)
	}
	err = upstreamStatusError(http.StatusForbidden, []byte(`{"code":"10605","message":"busy"}`))
	if !errors.As(err, &pe) || pe.RetryAfter != defaultQueueRetryAfter {
		t.Fatalf("missing retryAfterSeconds should use default: %+v", pe)
	}
	// A plain 403 is still an auth failure.
	err = upstreamStatusError(http.StatusForbidden, []byte(`{"code":"TOKEN_EXPIRE"}`))
	if !errors.As(err, &pe) || pe.Kind != accounts.KindAuth {
		t.Fatalf("plain 403 must stay auth: %+v", pe)
	}
}

func TestUpstreamStatusError430ClientVersionTooLow(t *testing.T) {
	err := upstreamStatusError(430, []byte("upgrade"))
	var pe *providers.Error
	if !errors.As(err, &pe) || pe.Code != providers.CodeClientVersionTooLow {
		t.Fatalf("430 not recognized: %v", err)
	}
	if pe.Message != clientVersionTooLowMessage {
		t.Fatalf("message=%q", pe.Message)
	}
}

func TestTrafficGuardDefaultOff(t *testing.T) {
	t.Setenv(trafficGuardEnv, "")
	cfg := trafficGuardConfigFromEnv()
	if cfg.enabled || cfg.concurrency != 1 || cfg.interval != 400*time.Millisecond {
		t.Fatalf("defaults=%+v", cfg)
	}
	t.Setenv(trafficGuardEnv, "on")
	t.Setenv(trafficGuardConcurrencyEnv, "2")
	t.Setenv(trafficGuardIntervalEnv, "50")
	cfg = trafficGuardConfigFromEnv()
	if !cfg.enabled || cfg.concurrency != 2 || cfg.interval != 50*time.Millisecond {
		t.Fatalf("configured=%+v", cfg)
	}
	var nilGuard *trafficGuard
	release, err := nilGuard.acquire(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestTrafficGuardSerializesAndSpacesPerAccount(t *testing.T) {
	g := newTrafficGuard(trafficGuardConfig{enabled: true, concurrency: 1, interval: 40 * time.Millisecond})
	var inflight, maxInflight int32
	var starts []time.Time
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := g.acquire(context.Background(), "acc")
			if err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&inflight, 1)
			for {
				m := atomic.LoadInt32(&maxInflight)
				if n <= m || atomic.CompareAndSwapInt32(&maxInflight, m, n) {
					break
				}
			}
			mu.Lock()
			starts = append(starts, time.Now())
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&inflight, -1)
			release()
		}()
	}
	wg.Wait()
	if maxInflight != 1 {
		t.Fatalf("max inflight=%d", maxInflight)
	}
	if total := starts[len(starts)-1].Sub(starts[0]); total < 70*time.Millisecond {
		t.Fatalf("starts not spaced: %v", total)
	}
	// Another account is independent.
	release, err := g.acquire(context.Background(), "other")
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestTrafficGuardHonorsContext(t *testing.T) {
	g := newTrafficGuard(trafficGuardConfig{enabled: true, concurrency: 1})
	hold, _ := g.acquire(context.Background(), "acc")
	defer hold()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := g.acquire(ctx, "acc"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}
