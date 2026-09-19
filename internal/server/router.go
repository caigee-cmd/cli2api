package server

import (
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/endpoint"
	"github.com/caigee-cmd/cli2api/internal/webui"
)

func (s *Server) routes() {
	s.mux.HandleFunc(endpoint.HealthPath, s.handleHealth)
	s.mux.HandleFunc("/api/overview", s.withConsoleKey(s.consoleHandler().HandleOverview))
	s.mux.HandleFunc("/api/overview/summary", s.withConsoleKey(s.consoleHandler().HandleOverviewSummary))
	s.mux.HandleFunc("/api/system/update", s.withConsoleKey(s.consoleHandler().HandleSystemUpdate))
	s.mux.HandleFunc("/api/system/update/prepare", s.withConsoleKey(s.consoleHandler().HandleSystemUpdatePrepare))
	s.mux.HandleFunc("/api/system/update/apply", s.withConsoleKey(s.consoleHandler().HandleSystemUpdateConfirm))
	s.mux.HandleFunc("/api/system/update/cancel", s.withConsoleKey(s.consoleHandler().HandleSystemUpdateCancel))
	s.mux.HandleFunc("/api/system/update/rollback", s.withConsoleKey(s.consoleHandler().HandleSystemUpdateRollback))
	s.mux.HandleFunc("/api/system/settings", s.withConsoleKey(s.consoleHandler().HandleSystemSettings))
	s.mux.HandleFunc("/api/system/console-key", s.withConsoleKey(s.consoleHandler().HandleConsoleKey))
	s.mux.HandleFunc("/api/keys", s.withConsoleKey(s.consoleHandler().HandleAPIKeys))
	s.mux.HandleFunc("/api/keys/", s.withConsoleKey(s.consoleHandler().HandleAPIKeyByID))
	s.mux.HandleFunc("/api/models", s.withConsoleKey(s.consoleHandler().HandleModelsAPI))
	s.mux.HandleFunc("/api/models/", s.withConsoleKey(s.consoleHandler().HandleModelSetting))
	s.mux.HandleFunc("/api/providers", s.withConsoleKey(s.consoleHandler().HandleProviders))
	s.mux.HandleFunc("/api/accounts", s.withConsoleKey(s.consoleHandler().HandleAccounts))
	s.mux.HandleFunc("/api/accounts/import", s.withConsoleKey(s.consoleHandler().HandleAccountImport))
	s.mux.HandleFunc("/api/accounts/", s.withConsoleKey(s.consoleHandler().HandleAccountByID))
	s.mux.HandleFunc("/api/logs", s.withConsoleKey(s.consoleHandler().HandleLogs))
	s.mux.HandleFunc("/api/logs/", s.withConsoleKey(s.consoleHandler().HandleLogs))
	s.mux.HandleFunc("/api/chat", s.withConsoleKey(s.consoleHandler().HandleChat))
	s.mux.HandleFunc(endpoint.ModelsPath, s.withAPIKey(s.gatewayHandler().HandleModels))
	s.mux.HandleFunc(endpoint.ChatCompletionsPath, s.withAPIKey(s.gatewayHandler().HandleChatCompletions))
	s.mux.HandleFunc(endpoint.MessagesPath, s.withAPIKey(s.gatewayHandler().HandleAnthropicMessages))
	s.mux.HandleFunc(endpoint.ResponsesPath, s.withAPIKey(s.gatewayHandler().HandleResponses))

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
