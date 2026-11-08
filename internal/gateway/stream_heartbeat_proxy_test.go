package gateway

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/executor"
)

// cfBudgetProxy is an in-process stand-in for Cloudflare's fixed response
// budget: it forwards bytes both ways, but aborts the client connection when the
// upstream has produced no response byte within budget. Any response byte --
// including an SSE comment frame -- satisfies the budget, which is how the real
// edge behaves (it counts bytes in flight, not semantic events).
type cfBudgetProxy struct {
	t        *testing.T
	listener net.Listener
	budget   time.Duration

	mu        sync.Mutex
	aborted   int
	completed int
}

func newCFBudgetProxy(t *testing.T, upstreamURL string, budget time.Duration) *cfBudgetProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := &cfBudgetProxy{t: t, listener: ln, budget: budget}
	target := strings.TrimPrefix(upstreamURL, "http://")
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			go p.handle(client, target)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return p
}

func (p *cfBudgetProxy) URL() string { return "http://" + p.listener.Addr().String() }

func (p *cfBudgetProxy) stats() (aborted, completed int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.aborted, p.completed
}

func (p *cfBudgetProxy) count(aborted bool) {
	p.mu.Lock()
	if aborted {
		p.aborted++
	} else {
		p.completed++
	}
	p.mu.Unlock()
}

func (p *cfBudgetProxy) handle(client net.Conn, target string) {
	_ = client.LocalAddr()
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		client.Close()
		return
	}
	defer client.Close()
	defer upstream.Close()

	// Request direction: plain forwarding.
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := client.Read(buf)
			if n > 0 {
				if _, werr := upstream.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if tcp, ok := upstream.(*net.TCPConn); ok {
					tcp.CloseWrite()
				}
				return
			}
		}
	}()

	// Response direction, with the budget applied only until the first byte.
	type readResult struct {
		n   int
		err error
	}
	buf := make([]byte, 32*1024)

	// readUpstream keeps exactly one blocking read outstanding so the budget
	// timer can win the race without leaking a goroutine per loop iteration.
	readUpstream := func() <-chan readResult {
		ch := make(chan readResult, 1)
		go func() {
			n, err := upstream.Read(buf)
			ch <- readResult{n, err}
		}()
		return ch
	}

	firstByte := false
	pending := readUpstream()
	startedAt := time.Now()
	deadline := time.NewTimer(p.budget)
	defer deadline.Stop()

	for {
		select {
		case <-deadline.C:
			if !firstByte {
				p.t.Logf("[proxy] budget expired at %v with no response byte", time.Since(startedAt))
				p.count(true)
				_, _ = client.Write([]byte("HTTP/1.1 524 <none>\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Length: 16\r\n\r\nerror code: 524\n"))
				return
			}
		case r := <-pending:
			if r.n > 0 {
				if !firstByte {
					firstByte = true
					deadline.Stop()
					p.t.Logf("[proxy] first response byte at %v: %q", time.Since(startedAt), string(buf[:min(r.n, 40)]))
				}
				if _, werr := client.Write(buf[:r.n]); werr != nil {
					return
				}
			}
			if r.err != nil {
				p.count(false)
				return
			}
			pending = readUpstream()
		}
	}
}

// The regression this change exists for: with a proxy budget shorter than the
// origin's time-to-first-byte, the unmodified gateway dies with 524 while the
// heartbeating gateway delivers the full stream.
func TestHeartbeatSurvivesProxyBudgetShorterThanUpstreamTTFB(t *testing.T) {
	const (
		// The env knobs floor at 1s, so the heartbeat commits at ~1s. Keep the
		// proxy budget above that and the upstream TTFB above both.
		proxyBudget = 2500 * time.Millisecond
		upstreamTTF = 5000 * time.Millisecond
	)

	for _, tc := range []struct {
		name        string
		heartbeatOn bool
		wantAborts  int
	}{
		{name: "with heartbeat", heartbeatOn: true, wantAborts: 0},
		{name: "without heartbeat", heartbeatOn: false, wantAborts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
				// Stand-in for a long-context prefill: nothing on the wire yet.
				time.Sleep(upstreamTTF)
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				w.WriteHeader(http.StatusOK)
				flusher := w.(http.Flusher)
				for i := 0; i < 3; i++ {
					_, _ = w.Write([]byte(`data: {"choices":[{"index":0,"delta":{"content":"tok"},"finish_reason":null}]}` + "\n\n"))
					flusher.Flush()
				}
				_, _ = w.Write([]byte("data: [DONE]\n\n"))
				flusher.Flush()
			}))
			defer upstream.Close()

			pool := executor.NewPool(nil, nil)
			pool.Upsert(executor.Item{ID: "acct", URL: upstream.URL, Provider: "qoder", Region: "global", Runtime: "child_process"})
			exec := executor.NewChatExecutor(pool, "")
			exec.HTTPClient = &http.Client{}
			handler := &Handler{Executor: exec, Pool: pool}

			if tc.heartbeatOn {
				t.Setenv(heartbeatEnvAfter, "150")
				t.Setenv(heartbeatEnvInterval, "100")
			} else {
				// Push the commit past the upstream's TTFB: the timer never
				// fires, which reproduces the pre-change behaviour.
				t.Setenv(heartbeatEnvAfter, "60000")
			}

			mux := http.NewServeMux()
			mux.HandleFunc("/v1/chat/completions", handler.HandleChatCompletions)
			gatewaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := auth.WithIdentity(r.Context(), auth.Identity{Kind: auth.KindKey, KeyID: "test"})
				mux.ServeHTTP(w, r.WithContext(ctx))
			}))
			defer gatewaySrv.Close()

			// Cloudflare sits between the caller and cli2api, so the proxy wraps
			// the gateway: that is the connection whose silence produces a 524.
			proxy := newCFBudgetProxy(t, gatewaySrv.URL, proxyBudget)

			resp, err := http.Post(proxy.URL()+"/v1/chat/completions", "application/json",
				strings.NewReader(`{"model":"qoder/mock","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			var sawKeepalive, sawDone bool
			reader := bufio.NewReader(resp.Body)
			deadline := time.Now().Add(20 * time.Second)
			for time.Now().Before(deadline) {
				line, err := reader.ReadString('\n')
				if strings.HasPrefix(line, ": keepalive") {
					sawKeepalive = true
				}
				if strings.HasPrefix(line, "data: [DONE]") {
					sawDone = true
					break
				}
				if err != nil {
					break
				}
			}

			aborted, _ := proxy.stats()
			if aborted != tc.wantAborts {
				t.Fatalf("proxy aborts = %d, want %d", aborted, tc.wantAborts)
			}
			if !tc.heartbeatOn {
				if sawDone {
					t.Fatal("without the heartbeat the stream should not have completed")
				}
				return
			}
			if !sawKeepalive {
				t.Fatal("client saw no keepalive frame")
			}
			if !sawDone {
				t.Fatal("client never received the end of the stream")
			}
		})
	}
}
