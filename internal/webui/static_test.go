package webui

import (
	"compress/gzip"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticAssetsUseImmutableCacheAndGzip(t *testing.T) {
	matches, err := fs.Glob(staticFS, "static/assets/*.js")
	if err != nil || len(matches) == 0 {
		t.Fatalf("find embedded js asset: %v (matches=%d)", err, len(matches))
	}
	name := strings.TrimPrefix(matches[0], "static/assets/")
	original, err := staticFS.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/assets/"+name, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != assetCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, assetCacheControl)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	reader, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("open gzip body: %v", err)
	}
	decoded, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	if string(decoded) != string(original) {
		t.Fatalf("decoded body = %d bytes, want %d", len(decoded), len(original))
	}

	plain := httptest.NewRequest(http.MethodGet, "/assets/"+name, nil)
	plainRec := httptest.NewRecorder()
	Handler().ServeHTTP(plainRec, plain)
	if got := plainRec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("plain Content-Encoding = %q, want empty", got)
	}
	if got := plainRec.Header().Get("Cache-Control"); got != assetCacheControl {
		t.Fatalf("plain Cache-Control = %q, want %q", got, assetCacheControl)
	}
}

func TestStaticMissIsNotCached(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/assets/missing-asset.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestIndexIsNeverCached(t *testing.T) {	rec := httptest.NewRecorder()
	if err := WriteIndex(rec); err != nil {
		t.Fatal(err)
	}
	if got := rec.Header().Get("Cache-Control"); got != indexCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, indexCacheControl)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html") {
		t.Fatalf("index body does not look like HTML: %q", rec.Body.String()[:min(80, rec.Body.Len())])
	}
}

func TestAcceptsGzipHonorsQuality(t *testing.T) {
	tests := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{"GZip", true},
		{"br, gzip;q=0.8", true},
		{"gzip;q=0", false},
		{"gzip; q=0.0", false},
		{"br", false},
	}
	for _, test := range tests {
		if got := acceptsGzip(test.header); got != test.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", test.header, got, test.want)
		}
	}
}
