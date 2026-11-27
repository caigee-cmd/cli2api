package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:static
var staticFS embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cacheControl := fileCacheControl
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			cacheControl = assetCacheControl
		}
		serveStatic(&cacheControlWriter{ResponseWriter: w, cacheControl: cacheControl}, r, files)
	})
}

func IndexHTML() ([]byte, error) {
	return staticFS.ReadFile("static/index.html")
}

// WriteIndex serves the SPA entry point. index.html is the only unhashed file
// in the bundle, so it must be revalidated on every navigation; otherwise a
// browser can keep pointing at asset hashes that no longer exist after a
// deploy.
func WriteIndex(w http.ResponseWriter) error {
	data, err := IndexHTML()
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", indexCacheControl)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err = w.Write(data)
	return err
}
