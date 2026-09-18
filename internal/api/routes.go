package api

import (
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/webui"
)

func (s *Server) routes() {
	s.mux.HandleFunc(endpoint.HealthPath, s.handleHealth)
	s.mux.HandleFunc("/api/overview", s.withConsoleKey(s.handleOverview))
	s.mux.HandleFunc("/api/overview/summary", s.withConsoleKey(s.handleOverviewSummary))
	s.mux.HandleFunc("/api/system/update", s.withConsoleKey(s.handleSystemUpdate))
	s.mux.HandleFunc("/api/system/update/prepare", s.withConsoleKey(s.handleSystemUpdatePrepare))
	s.mux.HandleFunc("/api/system/update/apply", s.withConsoleKey(s.handleSystemUpdateConfirm))
	s.mux.HandleFunc("/api/system/update/cancel", s.withConsoleKey(s.handleSystemUpdateCancel))
	s.mux.HandleFunc("/api/system/update/rollback", s.withConsoleKey(s.handleSystemUpdateRollback))
	s.mux.HandleFunc("/api/system/settings", s.withConsoleKey(s.handleSystemSettings))
	s.mux.HandleFunc("/api/system/console-key", s.withConsoleKey(s.handleConsoleKey))
	s.mux.HandleFunc("/api/keys", s.withConsoleKey(s.handleAPIKeys))
	s.mux.HandleFunc("/api/keys/", s.withConsoleKey(s.handleAPIKeyByID))
	s.mux.HandleFunc("/api/models", s.withConsoleKey(s.handleModelsAPI))
	s.mux.HandleFunc("/api/models/", s.withConsoleKey(s.handleModelSetting))
	s.mux.HandleFunc("/api/providers", s.withConsoleKey(s.handleProviders))
	s.mux.HandleFunc("/api/accounts", s.withConsoleKey(s.handleAccounts))
	s.mux.HandleFunc("/api/accounts/import", s.withConsoleKey(s.handleAccountImport))
	s.mux.HandleFunc("/api/accounts/", s.withConsoleKey(s.handleAccountByID))
	s.mux.HandleFunc("/api/logs", s.withConsoleKey(s.handleLogs))
	s.mux.HandleFunc("/api/logs/", s.withConsoleKey(s.handleLogs))
	s.mux.HandleFunc("/api/chat", s.withConsoleKey(s.handleChatCompletions))
	s.mux.HandleFunc(endpoint.ModelsPath, s.withAPIKey(s.handleModels))
	s.mux.HandleFunc(endpoint.ChatCompletionsPath, s.withAPIKey(s.handleChatCompletions))
	s.mux.HandleFunc(endpoint.MessagesPath, s.withAPIKey(s.handleAnthropicMessages))
	s.mux.HandleFunc(endpoint.ResponsesPath, s.withAPIKey(s.handleResponses))

	ui := webui.Handler()
	s.mux.Handle("/assets/", ui)
	s.mux.Handle("/favicon.svg", ui)
	s.mux.Handle("/favicon-dark.svg", ui)
	s.mux.Handle("/apple-touch-icon.svg", ui)
	s.mux.Handle("/og-card.svg", ui)
	s.mux.Handle("/site.webmanifest", ui)
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" &&
			!strings.HasPrefix(r.URL.Path, "/assets/") &&
			r.URL.Path != "/favicon.svg" &&
			r.URL.Path != "/favicon-dark.svg" &&
			r.URL.Path != "/apple-touch-icon.svg" &&
			r.URL.Path != "/og-card.svg" &&
			r.URL.Path != "/site.webmanifest" {
			switch r.URL.Path {
			case "/login", "/auth", "/providers", "/access", "/accounts", "/system", "/logs", "/keys":
			default:
				http.NotFound(w, r)
				return
			}
		}
		data, err := webui.IndexHTML()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}
