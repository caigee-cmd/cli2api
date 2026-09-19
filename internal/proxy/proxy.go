package proxy

import (
	"container/list"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	socksproxy "golang.org/x/net/proxy"
)

type Mode int

const (
	ModeInherit Mode = iota
	ModeDirect
	ModeProxy
)

// socksHandshakeTimeout bounds the TCP connect to the SOCKS proxy plus the
// SOCKS5 negotiation. Without it a stalled proxy with no caller deadline would
// leave the dial (and the standard library's fallback goroutine) blocked.
const socksHandshakeTimeout = 30 * time.Second

// socksDialContext dials through a SOCKS dialer using a context. Dialers that
// implement socksproxy.ContextDialer honour cancellation directly; the fallback
// closes a connection that arrives after the context is cancelled.
func socksDialContext(ctx context.Context, dialer socksproxy.Dialer, network, address string) (net.Conn, error) {
	if contextDialer, ok := dialer.(socksproxy.ContextDialer); ok {
		return contextDialer.DialContext(ctx, network, address)
	}
	type result struct {
		conn net.Conn
		err  error
	}
	done := make(chan result, 1)
	go func() {
		conn, err := dialer.Dial(network, address)
		done <- result{conn: conn, err: err}
	}()
	select {
	case <-ctx.Done():
		go func() {
			if r := <-done; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, ctx.Err()
	case r := <-done:
		if r.err == nil && ctx.Err() != nil {
			_ = r.conn.Close()
			return nil, ctx.Err()
		}
		return r.conn, r.err
	}
}

type Setting struct {
	Raw  string
	Mode Mode
	URL  *url.URL
}

func Parse(raw string) (Setting, error) {
	raw = strings.TrimSpace(raw)
	setting := Setting{Raw: raw}
	if raw == "" {
		return setting, nil
	}
	if strings.EqualFold(raw, "direct") || strings.EqualFold(raw, "none") {
		setting.Mode = ModeDirect
		return setting, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return Setting{Raw: raw}, fmt.Errorf("proxy URL must include a supported scheme and host")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "socks5", "socks5h":
		setting.Mode = ModeProxy
		setting.URL = parsed
		return setting, nil
	default:
		return Setting{Raw: raw}, fmt.Errorf("unsupported proxy scheme %q", parsed.Scheme)
	}
}

func Preserve(existing, replacement string) string {
	existing = strings.TrimSpace(existing)
	replacement = strings.TrimSpace(replacement)
	if existing != "" && Redact(existing) == replacement {
		return existing
	}
	return replacement
}

func Redact(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "direct") || strings.EqualFold(raw, "none") {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return raw
	}
	parsed.User = url.UserPassword(parsed.User.Username(), "******")
	return parsed.String()
}

func Effective(account, global string) string {
	if strings.TrimSpace(account) != "" {
		return strings.TrimSpace(account)
	}
	return strings.TrimSpace(global)
}

// ValidateHTTPOnly rejects SOCKS proxies while still accepting http(s),
// direct, none, and the empty (inherit) value. Child-process providers such
// as Qoder cannot route every cloud request through a SOCKS dialer, so their
// proxy settings must stay within the http(s) boundary.
func ValidateHTTPOnly(raw string) error {
	setting, err := Parse(raw)
	if err != nil {
		return err
	}
	if setting.Mode != ModeProxy {
		return nil
	}
	switch strings.ToLower(setting.URL.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("this proxy setting only supports http(s), direct, or none")
	}
}

func NewTransport(raw string) (*http.Transport, error) {
	setting, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	if setting.Mode == ModeInherit {
		return nil, nil
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok || transport == nil {
		transport = &http.Transport{}
	} else {
		transport = transport.Clone()
	}
	switch setting.Mode {
	case ModeDirect:
		transport.Proxy = nil
	case ModeProxy:
		if setting.URL.Scheme == "socks5" || setting.URL.Scheme == "socks5h" {
			var auth *socksproxy.Auth
			if setting.URL.User != nil {
				password, _ := setting.URL.User.Password()
				auth = &socksproxy.Auth{User: setting.URL.User.Username(), Password: password}
			}
			// The forward dialer dials the SOCKS proxy itself; use a
			// context-aware dialer so a dead proxy aborts on cancellation
			// instead of hanging in the standard library's goroutine fallback.
			dialer, err := socksproxy.SOCKS5("tcp", setting.URL.Host, auth, &net.Dialer{})
			if err != nil {
				return nil, fmt.Errorf("create SOCKS5 proxy: %w", err)
			}
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				// Bound the TCP connect + SOCKS handshake even when the caller
				// passes a context without a deadline, so a stalled proxy
				// cannot hold the dial (and its goroutine) open forever.
				handshakeCtx, cancel := context.WithTimeout(ctx, socksHandshakeTimeout)
				defer cancel()
				return socksDialContext(handshakeCtx, dialer, network, address)
			}
		} else {
			transport.Proxy = http.ProxyURL(setting.URL)
		}
	}
	return transport, nil
}

// maxCachedTransports bounds the cache. Without a limit, every distinct proxy
// URL (global or per-account) would keep an http.Transport alive forever,
// pinning its idle connections and proxy credentials even after the operator
// stops using that proxy. 32 comfortably covers realistic account counts while
// keeping reuse for the working set.
const maxCachedTransports = 32

// transportEntry is one cache slot: the key is stored alongside the transport
// so eviction can delete the map entry without a reverse lookup.
type transportEntry struct {
	raw       string
	transport *http.Transport
}

// TransportCache memoizes transports by proxy setting so repeated requests
// reuse the same connection pool (and HTTP CONNECT tunnel) instead of dialing
// and handshaking again. http.Transport is safe for concurrent use. It is a
// bounded LRU: the least-recently-used entry is evicted (and its idle
// connections closed) once maxCachedTransports is exceeded.
type TransportCache struct {
	mu         sync.Mutex
	transports map[string]*list.Element
	order      *list.List // most-recently-used at the front
}

// Get returns a cached transport for raw, building one lazily. An empty raw
// value yields (nil, nil) so callers keep their existing/default transport.
func (c *TransportCache) Get(raw string) (*http.Transport, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transports == nil {
		c.transports = make(map[string]*list.Element, maxCachedTransports)
	}
	if c.order == nil {
		c.order = list.New()
	}
	if element, ok := c.transports[raw]; ok {
		c.order.MoveToFront(element)
		return element.Value.(*transportEntry).transport, nil
	}

	transport, err := NewTransport(raw)
	if err != nil {
		return nil, err
	}
	if transport == nil {
		return nil, nil
	}
	c.transports[raw] = c.order.PushFront(&transportEntry{raw: raw, transport: transport})
	c.evictLocked()
	return transport, nil
}

// evictLocked drops least-recently-used entries until the cache is within
// capacity, closing their idle connections so old proxy tunnels do not linger.
func (c *TransportCache) evictLocked() {
	for c.order.Len() > maxCachedTransports {
		back := c.order.Back()
		if back == nil {
			return
		}
		entry := back.Value.(*transportEntry)
		c.order.Remove(back)
		delete(c.transports, entry.raw)
		entry.transport.CloseIdleConnections()
	}
}

// CloseIdleConnections closes idle connections on every cached transport.
func (c *TransportCache) CloseIdleConnections() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.order == nil {
		return
	}
	for element := c.order.Front(); element != nil; element = element.Next() {
		element.Value.(*transportEntry).transport.CloseIdleConnections()
	}
}
