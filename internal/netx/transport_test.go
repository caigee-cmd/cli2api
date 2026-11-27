package netx

import (
	"net/http"
	"testing"
	"time"
)

func TestTransportKeepsWideIdlePoolAndBoundsHeaderWait(t *testing.T) {
	transport := Transport()
	if transport.MaxIdleConnsPerHost != 100 {
		t.Fatalf("MaxIdleConnsPerHost = %d, want 100", transport.MaxIdleConnsPerHost)
	}
	if transport.MaxIdleConns < transport.MaxIdleConnsPerHost {
		t.Fatalf("MaxIdleConns = %d smaller than per-host pool", transport.MaxIdleConns)
	}
	if transport.MaxConnsPerHost != 0 {
		t.Fatalf("MaxConnsPerHost = %d, want unlimited", transport.MaxConnsPerHost)
	}
	if transport.ResponseHeaderTimeout != 5*time.Minute {
		t.Fatalf("ResponseHeaderTimeout = %s, want 5m", transport.ResponseHeaderTimeout)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("HTTP/2 disabled")
	}
}

func TestTransportStartsFromDefaultSettings(t *testing.T) {
	transport := Transport()
	if transport.Proxy == nil {
		t.Fatal("proxy function was not preserved")
	}
	// The standard transport is shared; the clone must not be the same object.
	if transport == http.DefaultTransport.(*http.Transport) {
		t.Fatal("Transport returned the shared default instead of a clone")
	}
}
