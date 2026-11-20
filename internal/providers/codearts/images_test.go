package codearts

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/translate"
)

func testPNG() []byte {
	// 1x1 white pixel PNG.
	raw, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/x8AAwMB/6Xv0AAAAABJRU5ErkJggg==")
	return raw
}

func TestInlineImageURLPassesThroughDataAndPlainValues(t *testing.T) {
	client := NewClient(&memoryStore{payload: mustTestCredential(t)})
	for _, value := range []string{"", "data:image/png;base64,AAAA", "not-a-url"} {
		got, err := inlineImageURL(context.Background(), client.http, client.images, value)
		if err != nil {
			t.Fatalf("inline(%q) error: %v", value, err)
		}
		if got != value {
			t.Fatalf("inline(%q) = %q", value, got)
		}
	}
}

func TestInlineImageURLDownloadsAndInlines(t *testing.T) {
	png := testPNG()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()

	client := NewClient(&memoryStore{payload: mustTestCredential(t)})
	got, err := inlineImageURL(context.Background(), client.http, client.images, server.URL+"/a.png")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("data URL = %q", got)
	}
	if decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "data:image/png;base64,")); err != nil || string(decoded) != string(png) {
		t.Fatalf("decoded payload mismatch (err=%v)", err)
	}
}

func TestInlineImageURLSniffsMimeWhenHeaderMissing(t *testing.T) {
	png := testPNG()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(png) // no Content-Type header
	}))
	defer server.Close()

	client := NewClient(&memoryStore{payload: mustTestCredential(t)})
	got, err := inlineImageURL(context.Background(), client.http, client.images, server.URL+"/a.bin")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("data URL = %q", got)
	}
}

func TestInlineImageURLUsesCacheOnSecondFetch(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG())
	}))
	defer server.Close()

	client := NewClient(&memoryStore{payload: mustTestCredential(t)})
	imageURL := server.URL + "/only.png"
	first, err := inlineImageURL(context.Background(), client.http, client.images, imageURL)
	if err != nil {
		t.Fatal(err)
	}
	second, err := inlineImageURL(context.Background(), client.http, client.images, imageURL)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("second fetch did not reuse the cached data URL")
	}
	if atomic.LoadInt32(&hits) != 1 {
		t.Fatalf("origin hits = %d, want 1", hits)
	}
}

func TestInlineImageURLFailsCleanlyOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(&memoryStore{payload: mustTestCredential(t)})
	_, err := inlineImageURL(context.Background(), client.http, client.images, server.URL+"/missing.png")
	if err == nil {
		t.Fatal("expected an error for a 404 image")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error = %v", err)
	}
}

func TestInlineMessageImagesRewritesPartsInPlace(t *testing.T) {
	png := testPNG()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	}))
	defer server.Close()

	client := NewClient(&memoryStore{payload: mustTestCredential(t)})
	messages := []translate.ChatMessage{{
		Role: "user",
		Content: []any{
			map[string]any{"type": "text", "text": "what is this?"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": server.URL + "/shot.png"}},
			map[string]any{"type": "image_url", "image_url": "data:image/png;base64,AAAA"},
		},
	}}
	if err := client.inlineMessageImages(context.Background(), "acct-1", messages); err != nil {
		t.Fatal(err)
	}
	parts := messages[0].Content.([]any)
	rewritten := parts[1].(map[string]any)["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(rewritten, "data:image/png;base64,") {
		t.Fatalf("rewritten url = %q", rewritten)
	}
	if got := parts[2].(map[string]any)["image_url"]; got != "data:image/png;base64,AAAA" {
		t.Fatalf("existing data URL was modified: %v", got)
	}
}
