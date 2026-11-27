package server

import (
	"compress/gzip"
	"net/http"
	"strconv"
	"strings"
)

// JSON API responses (logs, usage, account lists) can be large; static assets
// already have their own gzip path. Streaming endpoints (chat, console events)
// are deliberately excluded so SSE flushing is untouched.
func withJSONCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !compressJSONPath(r.URL.Path) || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		jw := &jsonResponseWriter{
			ResponseWriter: w,
			gzipAccepted:   acceptsGzip(r.Header.Get("Accept-Encoding")),
		}
		completed := false
		defer func() {
			// A panic must reach withRecovery before any header is committed;
			// otherwise recovery can no longer emit the structured JSON 500.
			if completed {
				_ = jw.Close()
			}
		}()
		next.ServeHTTP(jw, r)
		completed = true
	})
}

func compressJSONPath(path string) bool {
	if path == "/v1/models" {
		return true
	}
	if !strings.HasPrefix(path, "/api/") {
		return false
	}
	switch path {
	case "/api/events", "/api/chat":
		return false
	}
	return true
}

type jsonResponseWriter struct {
	http.ResponseWriter
	gzipAccepted bool
	status       int
	headerReady  bool
	committed    bool
	gz           *gzip.Writer
}

func (w *jsonResponseWriter) WriteHeader(status int) {
	if w.headerReady {
		return
	}
	w.status = status
	w.headerReady = true
}

func (w *jsonResponseWriter) Write(p []byte) (int, error) {
	if !w.headerReady {
		w.status = http.StatusOK
		w.headerReady = true
	}
	w.commit(true)
	if w.gz != nil {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

func (w *jsonResponseWriter) commit(allowCompress bool) {
	if w.committed {
		return
	}
	w.committed = true
	header := w.Header()
	contentType := header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		addVary(header, "Accept-Encoding")
	}
	if allowCompress &&
		w.gzipAccepted &&
		w.status != http.StatusNoContent &&
		w.status != http.StatusNotModified &&
		strings.HasPrefix(contentType, "application/json") &&
		header.Get("Content-Encoding") == "" {
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length")
		w.gz, _ = gzip.NewWriterLevel(w.ResponseWriter, gzip.BestSpeed)
	}
	w.ResponseWriter.WriteHeader(w.status)
}

func (w *jsonResponseWriter) Close() error {
	if !w.headerReady {
		w.status = http.StatusOK
		w.headerReady = true
	}
	if !w.committed {
		w.commit(false)
	}
	if w.gz != nil {
		return w.gz.Close()
	}
	return nil
}

func (w *jsonResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *jsonResponseWriter) Flush() {
	if !w.committed {
		w.commit(false)
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func addVary(header http.Header, value string) {
	for _, existing := range header.Values("Vary") {
		for _, item := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(item), value) {
				return
			}
		}
	}
	header.Add("Vary", value)
}

func acceptsGzip(value string) bool {
	for _, part := range strings.Split(value, ",") {
		fields := strings.Split(part, ";")
		if !strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			continue
		}
		q := 1.0
		for _, param := range fields[1:] {
			p := strings.TrimSpace(param)
			if !strings.HasPrefix(strings.ToLower(p), "q=") {
				continue
			}
			if v, err := strconv.ParseFloat(strings.TrimSpace(p[2:]), 64); err == nil {
				q = v
			}
		}
		if q > 0 {
			return true
		}
	}
	return false
}
