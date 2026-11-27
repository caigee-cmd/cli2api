package webui

import (
	"compress/gzip"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// Hashed Vite artifacts never change under the same URL, while index.html is
// the only file that points at a new hash. Serving those with different cache
// policies is what lets a deploy take effect on the next reload instead of
// leaving the browser holding a stale module graph.
const (
	assetCacheControl = "public, max-age=31536000, immutable"
	fileCacheControl  = "public, max-age=86400"
	indexCacheControl = "no-store"
)

var compressibleStaticExtensions = map[string]bool{
	".css":         true,
	".html":        true,
	".js":          true,
	".json":        true,
	".map":         true,
	".mjs":         true,
	".svg":         true,
	".txt":         true,
	".webmanifest": true,
	".xml":         true,
}

// cacheControlWriter applies the policy only once the handler has produced a
// successful response. A misspelled asset URL must not be cached as immutable
// or for a day; error responses are explicitly no-store.
type cacheControlWriter struct {
	http.ResponseWriter
	cacheControl string
	wroteHeader bool
}

func (w *cacheControlWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	switch {
	case status >= 200 && status < 300, status == http.StatusNotModified:
		w.Header().Set("Cache-Control", w.cacheControl)
	default:
		w.Header().Set("Cache-Control", "no-store")
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *cacheControlWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *cacheControlWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *cacheControlWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// serveStatic wraps the embedded file server with cache metadata and gzip for
// text assets. It deliberately skips Range requests: a byte range of a gzip
// stream is not a byte range of the original file, and the UI never needs
// ranges for its hashed bundles.
func serveStatic(w http.ResponseWriter, r *http.Request, next http.Handler) {
	ext := strings.ToLower(path.Ext(r.URL.Path))
	if !compressibleStaticExtensions[ext] {
		next.ServeHTTP(w, r)
		return
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if r.Method == http.MethodHead || r.Header.Get("Range") != "" || !acceptsGzip(r.Header.Get("Accept-Encoding")) {
		next.ServeHTTP(w, r)
		return
	}
	zw := &gzipResponseWriter{ResponseWriter: w}
	defer zw.Close()
	next.ServeHTTP(zw, r)
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}
		for _, param := range strings.Split(params, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
				continue
			}
			q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
			if err == nil && q <= 0 {
				return false
			}
		}
		return true
	}
	return false
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer      *gzip.Writer
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.wroteHeader = true
	if status == http.StatusOK {
		w.Header().Del("Content-Length")
		w.Header().Set("Content-Encoding", "gzip")
		w.writer = gzip.NewWriter(w.ResponseWriter)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.writer != nil {
		return w.writer.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *gzipResponseWriter) Flush() {
	if w.writer != nil {
		_ = w.writer.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *gzipResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *gzipResponseWriter) Close() error {
	if w.writer == nil {
		return nil
	}
	return w.writer.Close()
}
