package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		_ = s.manager.RefreshAll(r.Context())
		items, err := s.manager.Accounts(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "account_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items})
	case http.MethodPost:
		var input struct {
			Name        string `json:"name"`
			Enabled     bool   `json:"enabled"`
			MaxInFlight int    `json:"max_inflight"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		account, err := s.manager.Create(r.Context(), accounts.CreateAccount{Name: input.Name, Enabled: input.Enabled, MaxInFlight: input.MaxInFlight})
		if err != nil {
			writeErr(w, http.StatusBadRequest, "account_create_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, account)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
}

func (s *Server) handleAccountImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var input struct {
		Format    string `json:"format"`
		Name      string `json:"name"`
		Enabled   bool   `json:"enabled"`
		UserBlob  string `json:"user_blob"`
		MachineID string `json:"machine_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if input.Format != "qoder-native-v1" {
		writeErr(w, http.StatusBadRequest, "unsupported_credential_format", "format must be qoder-native-v1")
		return
	}
	blob, err := base64.StdEncoding.DecodeString(input.UserBlob)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_user_blob", "user_blob must be base64")
		return
	}
	account, err := s.manager.Import(r.Context(), accounts.ImportAccount{Name: input.Name, Enabled: input.Enabled, Credential: accounts.NativeCredential{UserBlob: blob, MachineID: input.MachineID}})
	if err != nil {
		writeErr(w, http.StatusBadRequest, "account_import_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (s *Server) handleAccountByID(w http.ResponseWriter, r *http.Request) {
	relative := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/accounts/"), "/")
	parts := strings.Split(relative, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeErr(w, http.StatusNotFound, "account_not_found", "account id required")
		return
	}
	accountID := parts[0]

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			account, err := s.manager.Store().Get(r.Context(), accountID)
			if err != nil {
				writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, account)
		case http.MethodPatch:
			var input struct {
				Name        string `json:"name"`
				Enabled     *bool  `json:"enabled"`
				MaxInFlight *int   `json:"max_inflight"`
				Priority    *int   `json:"priority"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			err := s.manager.Update(r.Context(), accountID, accounts.UpdateAccount{
				Name: input.Name, Enabled: input.Enabled, MaxInFlight: input.MaxInFlight, Priority: input.Priority,
			})
			if err != nil {
				writeErr(w, http.StatusBadRequest, "account_update_failed", err.Error())
				return
			}
			account, _ := s.manager.Store().Get(r.Context(), accountID)
			writeJSON(w, http.StatusOK, account)
		case http.MethodDelete:
			if err := s.manager.Delete(r.Context(), accountID); err != nil {
				writeErr(w, http.StatusBadRequest, "account_delete_failed", err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET, PATCH or DELETE only")
		}
		return
	}

	action := strings.Join(parts[1:], "/")
	switch action {
	case "export":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		account, err := s.manager.Store().Get(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		credential, err := s.manager.Store().LoadCredential(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "credential_not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"format": "qoder-native-v1", "name": account.Name,
			"user_blob":  base64.StdEncoding.EncodeToString(credential.UserBlob),
			"machine_id": credential.MachineID,
		})
	case "login/device":
		s.proxyAccountWorker(w, r, accountID, "/admin/login/device", "")
	case "login/status":
		s.proxyAccountWorker(w, r, accountID, "/admin/login/status", "oauth_if_complete")
	case "login/pat":
		s.proxyAccountWorker(w, r, accountID, "/admin/login/pat", "pat")
	case "rewarm":
		s.proxyAccountWorker(w, r, accountID, "/admin/rewarm", "")
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown account action")
	}
}
