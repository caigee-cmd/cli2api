package qoder

import (
	"context"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Traffic guard (qoder2api-hub 67aa3ce, QD_TRAFFIC_GUARD): an optional
// per-account gate that caps in-flight requests and spaces request starts, so
// the gateway itself never drives one account at an abnormal concurrency or
// cadence. Default OFF; configure with:
//
//	QODER_TRAFFIC_GUARD=on               enable (off by default)
//	QODER_TRAFFIC_GUARD_CONCURRENCY=1    max in-flight requests per account
//	QODER_TRAFFIC_GUARD_INTERVAL_MS=400  min gap between request starts
const (
	trafficGuardEnv            = "QODER_TRAFFIC_GUARD"
	trafficGuardConcurrencyEnv = "QODER_TRAFFIC_GUARD_CONCURRENCY"
	trafficGuardIntervalEnv    = "QODER_TRAFFIC_GUARD_INTERVAL_MS"

	defaultTrafficGuardConcurrency = 1
	defaultTrafficGuardInterval    = 400 * time.Millisecond
)

type trafficGuardConfig struct {
	enabled     bool
	concurrency int
	interval    time.Duration
}

func trafficGuardConfigFromEnv() trafficGuardConfig {
	cfg := trafficGuardConfig{concurrency: defaultTrafficGuardConcurrency, interval: defaultTrafficGuardInterval}
	switch strings.ToLower(strings.TrimSpace(os.Getenv(trafficGuardEnv))) {
	case "1", "on", "true", "yes", "enable", "enabled":
		cfg.enabled = true
	}
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(trafficGuardConcurrencyEnv))); err == nil && n > 0 {
		cfg.concurrency = n
	}
	if ms, err := strconv.Atoi(strings.TrimSpace(os.Getenv(trafficGuardIntervalEnv))); err == nil && ms >= 0 {
		cfg.interval = time.Duration(ms) * time.Millisecond
	}
	return cfg
}

type trafficGuard struct {
	cfg   trafficGuardConfig
	now   func() time.Time
	mu    sync.Mutex
	gates map[string]*accountGate
}

type accountGate struct {
	slots chan struct{}
	mu    sync.Mutex
	next  time.Time
}

func newTrafficGuard(cfg trafficGuardConfig) *trafficGuard {
	return &trafficGuard{cfg: cfg, now: time.Now, gates: map[string]*accountGate{}}
}

func (g *trafficGuard) gate(accountID string) *accountGate {
	g.mu.Lock()
	defer g.mu.Unlock()
	gate := g.gates[accountID]
	if gate == nil {
		gate = &accountGate{slots: make(chan struct{}, g.cfg.concurrency)}
		g.gates[accountID] = gate
	}
	return gate
}

// acquire blocks until the account has a free slot and the start interval
// has elapsed. The returned release must be called exactly once (it is
// idempotent). A nil or disabled guard returns a no-op release.
func (g *trafficGuard) acquire(ctx context.Context, accountID string) (func(), error) {
	if g == nil || !g.cfg.enabled {
		return func() {}, nil
	}
	gate := g.gate(accountID)
	select {
	case gate.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	var once sync.Once
	release := func() { once.Do(func() { <-gate.slots }) }
	if g.cfg.interval > 0 {
		gate.mu.Lock()
		now := g.now()
		wait := gate.next.Sub(now)
		start := now
		if wait > 0 {
			start = gate.next
		}
		gate.next = start.Add(g.cfg.interval)
		gate.mu.Unlock()
		if wait > 0 {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				release()
				return nil, ctx.Err()
			}
		}
	}
	return release, nil
}

// releaseOnClose returns the slot when the streamed body is closed.
type releaseOnClose struct {
	io.ReadCloser
	release func()
}

func (r releaseOnClose) Close() error {
	err := r.ReadCloser.Close()
	r.release()
	return err
}
