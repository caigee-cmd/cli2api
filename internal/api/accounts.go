package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/devin"
	"github.com/caigee-cmd/cli2api/internal/providers/trae"
	"github.com/caigee-cmd/cli2api/internal/providers/workbuddy"
)

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": providers.List()})
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.control.Accounts.List(r.Context(), r.URL.Query().Get("refresh") == "1")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "account_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": items})
	case http.MethodPost:
		var input struct {
			Name                 string `json:"name"`
			Provider             string `json:"provider"`
			Region               string `json:"region"`
			Enabled              bool   `json:"enabled"`
			MaxInFlight          int    `json:"max_inflight"`
			Priority             int    `json:"priority"`
			DropSystemPrompt     *bool  `json:"drop_system_prompt"`
			WorkBuddyAutoCheckin *bool  `json:"workbuddy_auto_checkin"`
			WorkBuddyCheckinTime string `json:"workbuddy_checkin_time"`
			ProxyURL             string `json:"proxy_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		if _, _, err := providers.Resolve(input.Provider, input.Region); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_provider", err.Error())
			return
		}
		account, err := s.control.Accounts.Create(r.Context(), accounts.CreateAccount{
			Name: input.Name, Provider: input.Provider, Region: input.Region,
			Enabled: input.Enabled, MaxInFlight: input.MaxInFlight, Priority: input.Priority,
			DropSystemPrompt: input.DropSystemPrompt, WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
			WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
		})
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
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var input struct {
		Format               string          `json:"format"`
		Name                 string          `json:"name"`
		Provider             string          `json:"provider"`
		Region               string          `json:"region"`
		Enabled              bool            `json:"enabled"`
		MaxInFlight          int             `json:"max_inflight"`
		Priority             int             `json:"priority"`
		DropSystemPrompt     *bool           `json:"drop_system_prompt"`
		WorkBuddyAutoCheckin *bool           `json:"workbuddy_auto_checkin"`
		WorkBuddyCheckinTime string          `json:"workbuddy_checkin_time"`
		ProxyURL             string          `json:"proxy_url"`
		UserBlob             string          `json:"user_blob"`
		MachineID            string          `json:"machine_id"`
		Credential           json.RawMessage `json:"credential"`
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	switch input.Format {
	case "qoder-native-v1":
		blob, err := base64.StdEncoding.DecodeString(input.UserBlob)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_user_blob", "user_blob must be base64")
			return
		}
		account, err := s.control.Accounts.ImportNative(r.Context(), accounts.ImportAccount{
			Name: input.Name, Provider: input.Provider, Region: input.Region, Enabled: input.Enabled,
			MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
			WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin, WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
			Credential: accounts.NativeCredential{UserBlob: blob, MachineID: input.MachineID},
		})
		if err != nil {
			writeErr(w, http.StatusBadRequest, "account_import_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, account)
	case trae.CredentialFormat:
		payload := input.Credential
		if len(payload) == 0 {
			payload = json.RawMessage(raw)
		}
		if err := trae.ValidateCredential(payload); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		credential, err := trae.DecodeCredential(payload)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		credential = trae.EnsureDevice(credential)
		encoded, err := credential.Encode()
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		imported, err := s.control.Accounts.ImportCredentialPayload(r.Context(), accounts.CreateAccount{
			Name: input.Name, Provider: "trae", Region: input.Region, Enabled: false,
			MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
			WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
			WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
		}, trae.CredentialFormat, encoded, credential.UID != "" && input.Enabled)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "account_import_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, imported)
	case workbuddy.CredentialFormat:
		payload := input.Credential
		if len(payload) == 0 {
			payload = json.RawMessage(raw)
		}
		if err := workbuddy.ValidateCredential(payload); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		var credential struct {
			UID string `json:"uid"`
		}
		_ = json.Unmarshal(payload, &credential)
		imported, err := s.control.Accounts.ImportCredentialPayload(r.Context(), accounts.CreateAccount{
			Name: input.Name, Provider: "workbuddy", Region: input.Region, Enabled: false,
			MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
			WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
			WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
		}, workbuddy.CredentialFormat, payload, credential.UID != "" && input.Enabled)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "account_import_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, imported)
	case devin.CredentialFormat:
		payload := input.Credential
		if len(payload) == 0 {
			payload = json.RawMessage(raw)
		}
		if err := devin.ValidateCredential(payload); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		credential, err := devin.DecodeCredential(payload)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		credential = devin.EnsureDeviceSeed(credential)
		encoded, err := credential.Encode()
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_credential", err.Error())
			return
		}
		imported, err := s.control.Accounts.ImportCredentialPayload(r.Context(), accounts.CreateAccount{
			Name: input.Name, Provider: "devin", Region: input.Region, Enabled: false,
			MaxInFlight: input.MaxInFlight, Priority: input.Priority, DropSystemPrompt: input.DropSystemPrompt,
			WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
			WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
		}, devin.CredentialFormat, encoded, credential.UserID != "" && input.Enabled)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "account_import_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, imported)
	default:
		writeErr(w, http.StatusBadRequest, "unsupported_credential_format", "format must be qoder-native-v1, workbuddy-oauth-v1, trae-oauth-v1, or devin-session-v1")
	}
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
			account, err := s.control.Accounts.Get(r.Context(), accountID)
			if err != nil {
				writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, account)
		case http.MethodPatch:
			var input struct {
				Name                 string  `json:"name"`
				Enabled              *bool   `json:"enabled"`
				MaxInFlight          *int    `json:"max_inflight"`
				Priority             *int    `json:"priority"`
				DropSystemPrompt     *bool   `json:"drop_system_prompt"`
				WorkBuddyAutoCheckin *bool   `json:"workbuddy_auto_checkin"`
				WorkBuddyCheckinTime *string `json:"workbuddy_checkin_time"`
				ProxyURL             *string `json:"proxy_url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			account, err := s.control.Accounts.Update(r.Context(), accountID, accounts.UpdateAccount{
				Name: input.Name, Enabled: input.Enabled, MaxInFlight: input.MaxInFlight, Priority: input.Priority,
				DropSystemPrompt: input.DropSystemPrompt, WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
				WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
			})
			if err != nil {
				writeErr(w, http.StatusBadRequest, "account_update_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, account)
		case http.MethodDelete:
			if err := s.control.Accounts.Delete(r.Context(), accountID); err != nil {
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
	// Refresh re-probes health, quota, and the model catalog for one account.
	// It must dispatch before the provider-native block below, which would
	// otherwise reject it as an unknown action for non-Qoder providers.
	if action == "refresh" {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		if _, err := s.control.Accounts.GetStored(r.Context(), accountID); err != nil {
			writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		if err := s.control.Accounts.RefreshAccount(r.Context(), accountID, r.URL.Query().Get("quota") == "1"); err != nil {
			writeErr(w, http.StatusBadGateway, "account_refresh_failed", err.Error())
			return
		}
		view, err := s.control.Accounts.Get(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "account_view_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	if action == "checkins" {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		account, err := s.control.Accounts.GetStored(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		if account.Provider != "workbuddy" {
			writeErr(w, http.StatusBadRequest, "provider_unsupported", "check-in is only available for WorkBuddy accounts")
			return
		}
		records, err := s.control.Accounts.ListCheckins(r.Context(), accountID, 20)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "checkin_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": records})
		return
	}
	if account, err := s.control.Accounts.GetStored(r.Context(), accountID); err == nil && action == "checkin" {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		if account.Provider != "workbuddy" {
			writeErr(w, http.StatusBadRequest, "provider_unsupported", "check-in is only available for WorkBuddy accounts")
			return
		}
		updated, err := s.control.Accounts.Checkin(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "checkin_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
		return
	}
	// Provider-native actions dispatch before the Qoder worker proxy.
	if account, err := s.control.Accounts.GetStored(r.Context(), accountID); err == nil && account.Provider != "qoder" {
		adapter, ok := s.providers.Get(account.Provider)
		if !ok || adapter.Login == nil {
			writeErr(w, http.StatusBadRequest, "provider_unsupported", "provider does not support this action")
			return
		}
		switch action {
		case "login/device":
			if r.Method != http.MethodPost {
				writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
				return
			}
			session, err := adapter.Login.StartLogin(r.Context(), accountID)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "login_start_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"authUrl": session.AuthURL, "status": "pending"})
			return
		case "login/status":
			if r.Method != http.MethodGet {
				writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
				return
			}
			done, message, err := adapter.Login.PollLogin(r.Context(), accountID)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "login_poll_failed", err.Error())
				return
			}
			status := "pending"
			if done {
				status = "ok"
			}
			writeJSON(w, http.StatusOK, map[string]any{"login": map[string]any{"status": status, "message": message}})
			return
		case "login/callback":
			if r.Method != http.MethodPost {
				writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
				return
			}
			completer, ok := adapter.Login.(providers.LoginCompleter)
			if !ok {
				writeErr(w, http.StatusBadRequest, "provider_unsupported", "provider does not accept a pasted callback URL")
				return
			}
			var input struct {
				CallbackURL string `json:"callback_url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			if err := completer.CompleteLogin(r.Context(), accountID, input.CallbackURL); err != nil {
				writeErr(w, http.StatusBadRequest, "login_callback_failed", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"login": map[string]any{"status": "ok", "message": "login complete"}})
			return
		case "export":
			if r.Method != http.MethodGet {
				writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
				return
			}
			format, payload, err := s.control.Accounts.LoadCredentialPayload(r.Context(), accountID)
			if err != nil {
				writeErr(w, http.StatusNotFound, "credential_not_found", err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"format": format, "name": account.Name, "credential": json.RawMessage(payload),
			})
			return
		default:
			writeErr(w, http.StatusNotFound, "not_found", "unknown account action")
			return
		}
	}
	switch action {
	case "export":
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		account, err := s.control.Accounts.GetStored(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		credential, err := s.control.Accounts.LoadNativeCredential(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "credential_not_found", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"format": "qoder-native-v1", "name": account.Name,
			"provider":   account.Provider,
			"region":     account.ProviderRegion,
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
