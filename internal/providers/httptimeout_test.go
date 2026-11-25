package providers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDoWithHeadTimeoutFailsWhenUpstreamStaysSilent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = DoWithHeadTimeout(server.Client(), req, 50*time.Millisecond)
	if err == nil {
		t.Fatal("want a timeout error")
	}
	if !errors.Is(err, ErrFirstByteTimeout) {
		t.Fatalf("err = %v, want ErrFirstByteTimeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("head timeout took %s, want a fast fail", elapsed)
	}
}

func TestDoWithHeadTimeoutKeepsBodyReadable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: ok\n\n"))
	}))
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := DoWithHeadTimeout(server.Client(), req, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("successful head must not fail: %v", err)
	}
	defer resp.Body.Close()
	// The derived deadline must not cut the body after the head arrives.
	time.Sleep(150 * time.Millisecond)
	if _, err := resp.Body.Read(make([]byte, 16)); err != nil && !strings.Contains(err.Error(), "EOF") {
		t.Fatalf("body read after head: %v", err)
	}
}

func TestHeadTimeoutFromEnvClampsAndDefaults(t *testing.T) {
	t.Setenv("TEST_TTFB_MS", "")
	if got := HeadTimeoutFromEnv("TEST_TTFB_MS", 0); got != DefaultStreamHeadTimeout {
		t.Fatalf("default = %s", got)
	}
	t.Setenv("TEST_TTFB_MS", "1200")
	if got := HeadTimeoutFromEnv("TEST_TTFB_MS", 0); got != 5*time.Second {
		t.Fatalf("sub-5s value must clamp up, got %s", got)
	}
	t.Setenv("TEST_TTFB_MS", "15000")
	if got := HeadTimeoutFromEnv("TEST_TTFB_MS", 0); got != 15*time.Second {
		t.Fatalf("explicit value = %s", got)
	}
}
