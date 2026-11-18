package auth

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"time"
)

// LoopbackRoute is one path a loopback listener accepts. A provider that gets
// two redirects on the same port (Kiro's Microsoft sign-in does) registers both
// here instead of building its own server.
type LoopbackRoute struct {
	Path     string
	Title    string
	Callback func(context.Context, string) error
}

// ServeLoopback owns the HTTP transport on an already-bound loopback listener.
// Provider callbacks receive protocol input only; errors preserve the 400 contract.
func ServeLoopback(ln net.Listener, path, title string, callback func(context.Context, string) error) {
	ServeLoopbackRoutes(ln, []LoopbackRoute{{Path: path, Title: title, Callback: callback}})
}

// ServeLoopbackRoutes is ServeLoopback for a listener that answers more than
// one path. One listener can only be served once, so the routes share it.
func ServeLoopbackRoutes(ln net.Listener, routes []LoopbackRoute) {
	mux := http.NewServeMux()
	for _, route := range routes {
		mux.HandleFunc(route.Path, loopbackHandler(route.Title, route.Callback))
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
}

func loopbackHandler(title string, callback func(context.Context, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := callback(r.Context(), r.URL.String()); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, fmt.Sprintf(`<!doctype html><meta charset="utf-8"><title>%s login</title><p>Login complete. You can close this tab.</p>`, html.EscapeString(title)))
	}
}
