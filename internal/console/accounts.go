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
			Name                 string   `json:"name"`
			Provider             string   `json:"provider"`
			Region               string   `json:"region"`
			Enabled              bool     `json:"enabled"`
			MaxInFlight          int      `json:"max_inflight"`
			Priority             int      `json:"priority"`
			CreditFloor          *float64 `json:"credit_floor,omitempty"`
			PercentFloor         *float64 `json:"percent_floor,omitempty"`
			DropSystemPrompt     *bool    `json:"drop_system_prompt"`
			WorkBuddyAutoCheckin *bool    `json:"workbuddy_auto_checkin"`
			WorkBuddyCheckinTime string   `json:"workbuddy_checkin_time"`
			AutoCheckin          *bool    `json:"auto_checkin"`
			CheckinTime          string   `json:"checkin_time"`
			ProxyURL             string   `json:"proxy_url"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil {
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
			CreditFloor:      input.CreditFloor,
			PercentFloor:     input.PercentFloor,
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

func (h *Handler) HandleCheckinAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var input struct {
		Force           bool  `json:"force"`
		IncludeDisabled *bool `json:"include_disabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	includeDisabled := false
	if h.System != nil {
		includeDisabled = h.System.Current(r.Context()).CheckinDisabledAccounts
	}
	if input.IncludeDisabled != nil {
		includeDisabled = *input.IncludeDisabled
	}
	results, err := h.Control.Accounts.CheckinAll(r.Context(), input.Force, includeDisabled)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": results})
}

// HandleRefreshAll refreshes every account concurrently (full fan-out) and
// returns each account's real outcome, including the verbatim upstream error
// and the fresh quota snapshot. Disabled accounts are included only when the
// caller asks for them or the system setting enables it.
func (h *Handler) HandleRefreshAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var input struct {
		Quota           *bool `json:"quota"`
		IncludeDisabled *bool `json:"include_disabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil && err != io.EOF {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	forceQuota := true
	if input.Quota != nil {
		forceQuota = *input.Quota
	}
	includeDisabled := false
	if input.IncludeDisabled != nil {
		includeDisabled = *input.IncludeDisabled
	}
	results := h.Control.Accounts.RefreshAllResults(r.Context(), forceQuota, includeDisabled)
	if results == nil {
		results = []accounts.AccountRefreshResult{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": results})
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
	result, err := h.Control.Accounts.ImportAll(r.Context(), input, raw)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	// Single-credential imports keep the historical account body so existing
	// clients (and the add-account wizard) do not need a shape change.
	if result.Succeeded == 1 && result.Failed == 0 && len(result.Data) == 1 {
		writeJSON(w, http.StatusCreated, result.Data[0])
		return
	}
	status := http.StatusCreated
	if result.Failed > 0 {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

func (h *Handler) HandleAccountBundleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
		return
	}
	bundle, err := h.Control.Accounts.ExportBundle(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "account_export_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (h *Handler) HandleAccountBundleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	var bundle control.AccountBundle
	if err = json.Unmarshal(raw, &bundle); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := h.Control.Accounts.ImportBundle(r.Context(), bundle)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Failed > 0 {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
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
			var raw json.RawMessage
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&raw); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			var input struct {
				Name                 string            `json:"name"`
				Enabled              *bool             `json:"enabled"`
				MaxInFlight          *int              `json:"max_inflight"`
				Priority             *int              `json:"priority"`
				CreditFloor          *float64          `json:"credit_floor"`
				PercentFloor         *float64          `json:"percent_floor"`
				DropSystemPrompt     *bool             `json:"drop_system_prompt"`
				WorkBuddyAutoCheckin *bool             `json:"workbuddy_auto_checkin"`
				WorkBuddyCheckinTime *string           `json:"workbuddy_checkin_time"`
				AutoCheckin          *bool             `json:"auto_checkin"`
				CheckinTime          *string           `json:"checkin_time"`
				ProxyURL             *string           `json:"proxy_url"`
				ModelAliases         map[string]string `json:"model_aliases"`
				EnabledModels        []string          `json:"enabled_models"`
				SetModelRouting      bool              `json:"set_model_routing"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			account, err := h.Control.Accounts.Update(r.Context(), accountID, accounts.UpdateAccount{
				AutoCheckin: input.AutoCheckin, CheckinTime: input.CheckinTime,
				Name: input.Name, Enabled: input.Enabled, MaxInFlight: input.MaxInFlight, Priority: input.Priority,
				CreditFloor:      input.CreditFloor,
				PercentFloor:     input.PercentFloor,
				SetCreditFloor:   hasJSONField(raw, "credit_floor"),
				SetPercentFloor:  hasJSONField(raw, "percent_floor"),
				DropSystemPrompt: input.DropSystemPrompt, WorkBuddyAutoCheckin: input.WorkBuddyAutoCheckin,
				WorkBuddyCheckinTime: input.WorkBuddyCheckinTime, ProxyURL: input.ProxyURL,
				ModelAliases: input.ModelAliases, EnabledModels: input.EnabledModels, SetModelRouting: input.SetModelRouting,
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
	var callbackURL, loginMethod, loginStartURL string
	if action == "login/device" || action == "login/callback" {
		var input struct {
			CallbackURL string `json:"callback_url"`
			LoginMethod string `json:"login_method"`
			StartURL    string `json:"start_url"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConsoleBodyBytes)).Decode(&input); err != nil && err != io.EOF {
			writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		callbackURL = input.CallbackURL
		loginMethod = input.LoginMethod
		loginStartURL = input.StartURL
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	result, err := h.Control.Accounts.Admin(r.Context(), control.AccountAdminAction{
		AccountID: accountID, Action: action, Method: r.Method, ContentType: r.Header.Get("Content-Type"), Body: body, CallbackURL: callbackURL, LoginMethod: loginMethod, LoginStartURL: loginStartURL,
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

// hasJSONField reports whether a JSON object contains a key, including when
// its value is null. It lets floor overrides distinguish "inherit" (null or
// absent) from an explicit numeric value without changing PATCH semantics.
func hasJSONField(raw json.RawMessage, key string) bool {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return false
	}
	_, ok := obj[key]
	return ok
}
