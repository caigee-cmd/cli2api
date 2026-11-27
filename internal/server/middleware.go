package server

import (
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/endpoint"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

func (s *Server) Handler() http.Handler {
	return withSecurityHeaders(withJSONCompression(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	})))
}

const contentSecurityPolicy = "default-src 'self'; base-uri 'self'; object-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'self'; form-action 'self'"

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Content-Security-Policy", contentSecurityPolicy)
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "SAMEORIGIN")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
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
		client := clientIP(r)
		if blocked, retry := s.authFailures.blocked(client); blocked {
			writeAuthBlocked(w, retry)
			return
		}
		identity, ok := s.Auth.Authenticate(r.Context(), r)
		if !ok {
			if credentialsPresented(r) {
				if blocked, retry := s.authFailures.failure(client); blocked {
					writeAuthBlocked(w, retry)
					return
				}
			}
			writeErr(w, http.StatusUnauthorized, "invalid_api_key", "Missing/invalid API key")
			return
		}
		s.authFailures.success(client)
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
