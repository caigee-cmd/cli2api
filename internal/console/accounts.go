package console

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (h *Handler) HandleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": providers.List()})
}

func (h *Handler) HandleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := h.Control.Accounts.List(r.Context(), r.URL.Query().Get("refresh") == "1")
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
			AutoCheckin          *bool  `json:"auto_checkin"`
			CheckinTime          string `json:"checkin_time"`
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
		account, err := h.Control.Accounts.Create(r.Context(), accounts.CreateAccount{
			AutoCheckin: input.AutoCheckin, CheckinTime: input.CheckinTime,
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

func (h *Handler) HandleAccountImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var input control.AccountImportInput
	if err = json.Unmarshal(raw, &input); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	account, err := h.Control.Accounts.Import(r.Context(), input, raw)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, account)
}

func (h *Handler) HandleAccountByID(w http.ResponseWriter, r *http.Request) {
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
			account, err := h.Control.Accounts.Get(r.Context(), accountID)
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
				AutoCheckin          *bool   `json:"auto_checkin"`
				CheckinTime          *string `json:"checkin_time"`
				ProxyURL             *string `json:"proxy_url"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			account, err := h.Control.Accounts.Update(r.Context(), accountID, accounts.UpdateAccount{
				AutoCheckin: input.AutoCheckin, CheckinTime: input.CheckinTime,
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
			if err := h.Control.Accounts.Delete(r.Context(), accountID); err != nil {
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
	if action == "refresh" {
		if r.Method != http.MethodPost {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}
		if _, err := h.Control.Accounts.GetStored(r.Context(), accountID); err != nil {
			writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
			return
		}
		if err := h.Control.Accounts.RefreshAccount(r.Context(), accountID, r.URL.Query().Get("quota") == "1"); err != nil {
			writeErr(w, http.StatusBadGateway, "account_refresh_failed", err.Error())
			return
		}
		view, err := h.Control.Accounts.Get(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "account_view_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, view)
		return
	}
	required := map[string]string{
		"checkins":       http.MethodGet,
		"checkin":        http.MethodPost,
		"login/device":   http.MethodPost,
		"login/status":   http.MethodGet,
		"login/callback": http.MethodPost,
	}
	if want, ok := required[action]; ok && r.Method != want {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", want+" only")
		return
	}
	if action == "export" {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		exported, err := h.Control.Accounts.Export(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusNotFound, "credential_not_found", err.Error())
			return
		}
		payload := map[string]any{"format": exported.Format, "name": exported.Name}
		if exported.Format == "qoder-native-v1" {
			payload["provider"] = exported.Provider
			payload["region"] = exported.Region
			payload["user_blob"] = exported.UserBlob
			payload["machine_id"] = exported.MachineID
		} else {
			payload["credential"] = json.RawMessage(exported.Credential)
		}
		writeJSON(w, http.StatusOK, payload)
		return
	}
	var callbackURL string
	if action == "login/callback" {
		var input struct {
			CallbackURL string `json:"callback_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		callbackURL = input.CallbackURL
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := h.Control.Accounts.Admin(r.Context(), control.AccountAdminAction{
		AccountID: accountID, Action: action, Method: r.Method, ContentType: r.Header.Get("Content-Type"), Body: body, CallbackURL: callbackURL,
	})
	if err != nil {
		writeOperationError(w, err)
		return
	}
	switch result.Kind {
	case "checkins":
		records, err := h.Control.Accounts.ListCheckins(r.Context(), accountID, 20)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "checkin_list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": records})
	case "checkin":
		updated, err := h.Control.Accounts.Checkin(r.Context(), accountID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "checkin_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	case "login_start":
		writeJSON(w, http.StatusOK, map[string]any{"authUrl": result.Session.AuthURL, "status": result.LoginStatus})
	case "login_status":
		writeJSON(w, http.StatusOK, map[string]any{"login": map[string]any{"status": result.LoginStatus, "message": result.LoginMsg}})
	case "login_complete":
		writeJSON(w, http.StatusOK, map[string]any{"login": map[string]any{"status": result.LoginStatus, "message": result.LoginMsg}})
	case "worker":
		for key, values := range result.Worker.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(result.Worker.Status)
		_, _ = w.Write(result.Worker.Body)
	default:
		writeErr(w, http.StatusNotFound, "not_found", "unknown account action")
	}
}
