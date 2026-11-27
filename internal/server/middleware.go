package server

import (
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.draining() && !isLivenessPath(r.URL.Path) {
			w.Header().Set("Connection", "close")
			writeErr(w, http.StatusServiceUnavailable, "service_draining", "Service is shutting down")
			return
		}
		if isOpenAIEndpoint(r.URL.Path) {
			setOpenAICORSHeaders(w, r)
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if s.updater().Maintenance.Load() && appupdate.BlocksDuringUpdate(r.URL.Path) {
			writeErr(w, http.StatusServiceUnavailable, "service_updating", "Service update in progress")
			return
		}
		s.mux.ServeHTTP(w, r)
	})
}

// isLivenessPath keeps probes usable while the process drains; everything else
// is new work and gets a clean 503 instead of a connection reset.
func isLivenessPath(path string) bool {
	return path == endpoint.HealthPath || path == endpoint.ReadyPath
}

func isOpenAIEndpoint(path string) bool {
	switch path {
	case endpoint.ModelsPath, endpoint.ChatCompletionsPath, endpoint.MessagesPath, endpoint.ResponsesPath:
		return true
	default:
		return false
	}
}

func setOpenAICORSHeaders(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", "*")
	header.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	requestedHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
	if requestedHeaders == "" {
		requestedHeaders = "Authorization, Content-Type, X-API-Key, X-Requested-With"
	}
	header.Set("Access-Control-Allow-Headers", requestedHeaders)
	header.Set("Access-Control-Expose-Headers", "X-Request-Id, X-Qoder-Account, X-CLI2API-Account, X-CLI2API-Provider")
	header.Set("Access-Control-Max-Age", "600")
}

func (s *Server) withAPIKey(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, ok := s.Auth.Authenticate(r.Context(), r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "invalid_api_key", "Missing/invalid API key")
			return
		}
		if identity.Kind == auth.KindKey && s.TouchKey != nil {
			s.TouchKey(r.Context(), identity.KeyID)
		}
		next(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
	}
}

func (s *Server) withConsoleKey(next http.HandlerFunc) http.HandlerFunc {
	return s.withAPIKey(func(w http.ResponseWriter, r *http.Request) {
		identity, _ := auth.IdentityFrom(r.Context())
		if !identity.Console() {
			writeErr(w, http.StatusForbidden, "console_key_required", "This endpoint requires the console API key")
			return
		}
		next(w, r)
	})
}
