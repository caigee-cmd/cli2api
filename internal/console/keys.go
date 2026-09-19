package console

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type consoleKeyView struct {
	Prefix  string `json:"prefix"`
	Hint    string `json:"hint"`
	Rotated bool   `json:"rotated,omitempty"`
	Secret  string `json:"secret,omitempty"`
}

func (h *Handler) HandleAPIKeys(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		keys, err := h.Control.Keys.List(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "api_key_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": keys})
	case http.MethodPost:
		var input struct {
			Name      string   `json:"name"`
			Providers []string `json:"providers"`
			Enabled   *bool    `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		enabled := true
		if input.Enabled != nil {
			enabled = *input.Enabled
		}
		key, err := h.Control.Keys.Create(r.Context(), accounts.CreateAPIKey{
			Name: input.Name, Providers: input.Providers, Enabled: enabled,
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, "api_key_create_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, key)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
}

func (h *Handler) HandleAPIKeyByID(w http.ResponseWriter, r *http.Request) {
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/keys/"), "/")
	if id == "" || strings.Contains(id, "/") {
		writeErr(w, http.StatusNotFound, "api_key_not_found", "api key id required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		key, err := h.Control.Keys.Get(r.Context(), id)
		if errors.Is(err, accounts.ErrAPIKeyNotFound) {
			writeErr(w, http.StatusNotFound, "api_key_not_found", err.Error())
			return
		}
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "api_key_get_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, key)
	case http.MethodPatch:
		var input struct {
			Name      string   `json:"name"`
			Providers []string `json:"providers"`
			Enabled   *bool    `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		key, err := h.Control.Keys.Update(r.Context(), id, accounts.UpdateAPIKey{
			Name: input.Name, Providers: input.Providers, Enabled: input.Enabled,
		})
		if errors.Is(err, accounts.ErrAPIKeyNotFound) {
			writeErr(w, http.StatusNotFound, "api_key_not_found", err.Error())
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadRequest, "api_key_update_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, key)
	case http.MethodDelete:
		if err := h.Control.Keys.Delete(r.Context(), id); errors.Is(err, accounts.ErrAPIKeyNotFound) {
			writeErr(w, http.StatusNotFound, "api_key_not_found", err.Error())
			return
		} else if err != nil {
			writeErr(w, http.StatusInternalServerError, "api_key_delete_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET, PATCH, or DELETE only")
	}
}

func (h *Handler) HandleConsoleKey(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		secret := h.cfgProxyAPIKey()
		writeJSON(w, http.StatusOK, consoleKeyView{
			Prefix: accounts.APIKeyPrefix(secret),
			Hint:   "This key unlocks the console and can call every provider.",
		})
	case http.MethodPost:
		var input struct {
			Rotate bool `json:"rotate"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil && err != io.EOF {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if !input.Rotate {
			writeErr(w, http.StatusBadRequest, "invalid_request", "set rotate=true to mint a new console key")
			return
		}
		secret, err := h.KeyRotation.Rotate(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "console_key_rotate_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, consoleKeyView{
			Prefix:  accounts.APIKeyPrefix(secret),
			Hint:    "Store this value now. The console will ask for it on the next sign-in.",
			Rotated: true,
			Secret:  secret,
		})
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
}
