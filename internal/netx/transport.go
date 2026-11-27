// Package netx owns the process-wide HTTP transport defaults.
//
// The gateway fans many concurrent requests out to a handful of upstream hosts.
// Go's default transport keeps only two idle connections per host, so a burst
// of streams closes and reopens TLS connections continuously. Every reconnect
// adds a handshake to the caller's first byte and burns CPU on both sides. The
// transport here keeps a wider idle pool and bounds the response-head wait, so
// an upstream that accepts a connection and never answers cannot pin a
// request goroutine forever.
package netx

import (
	"net/http"
	"time"
)

const (
	maxIdleConns        = 200
	maxIdleConnsPerHost = 100
	idleConnTimeout     = 90 * time.Second
	// responseHeaderTimeout mirrors the stream idle deadline. A non-streaming
	// upstream may legitimately spend minutes generating before it sends headers,
	// so this must stay generous; it exists to stop an indefinite hang, not to
	// police normal latency.
	responseHeaderTimeout = 5 * time.Minute
)

// Transport returns a tuned clone of the standard transport. Callers must not
// mutate the returned value after handing it to a client.
func Transport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		base = &http.Transport{Proxy: http.ProxyFromEnvironment}
	} else {
		base = base.Clone()
	}
	base.MaxIdleConns = maxIdleConns
	base.MaxIdleConnsPerHost = maxIdleConnsPerHost
	// Zero means no per-host cap: streams are long-lived and a low cap would
	// queue new requests behind established ones.
	base.MaxConnsPerHost = 0
	base.IdleConnTimeout = idleConnTimeout
	base.ResponseHeaderTimeout = responseHeaderTimeout
	base.ExpectContinueTimeout = time.Second
	base.ForceAttemptHTTP2 = true
	return base
}
