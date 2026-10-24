package workbuddy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

func TestResolveFirstByteTimeout(t *testing.T) {
	t.Setenv(workbuddyStreamTTFBEnv, "")
	if got := resolveFirstByteTimeout(); got != defaultFirstByteTimeout {
		t.Fatalf("default = %s, want %s", got, defaultFirstByteTimeout)
	}

	t.Setenv(workbuddyStreamTTFBEnv, "45000")
	if got := resolveFirstByteTimeout(); got != 45*time.Second {
		t.Fatalf("override = %s, want 45s", got)
	}

	// Below the floor must clamp up, otherwise normal latency looks like a hang.
	t.Setenv(workbuddyStreamTTFBEnv, "100")
	if got := resolveFirstByteTimeout(); got != 5*time.Second {
		t.Fatalf("floor = %s, want 5s", got)
	}

	// Invalid values fall back to the default rather than disabling the guard.
	for _, bad := range []string{"0", "-1", "soon", "  "} {
		t.Setenv(workbuddyStreamTTFBEnv, bad)
		if got := resolveFirstByteTimeout(); got != defaultFirstByteTimeout {
			t.Fatalf("value %q = %s, want default %s", bad, got, defaultFirstByteTimeout)
		}
	}
}

func TestChatStreamFailsOverWhenHeadNeverArrives(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Accept the request, then never write a head — the upstream hang this
		// guard exists for.
		<-release
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = doWithFirstByteTimeout(server.Client(), req, 150*time.Millisecond)
	if err == nil {
		t.Fatal("expected a first-byte timeout")
	}
	if !errors.Is(err, providers.ErrFirstByteTimeout) {
		t.Fatalf("err = %v, want ErrFirstByteTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("waited %s, the deadline did not fire", elapsed)
	}
}

func TestChatStreamKeepsBodyAliveAfterHead(t *testing.T) {
	// A head that arrives in time must leave the body readable: cancelling the
	// deadline context after success would truncate the stream.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := doWithFirstByteTimeout(server.Client(), req, 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, _ := resp.Body.Read(buf)
	if n == 0 {
		t.Fatal("body was truncated after a successful head")
	}
}
