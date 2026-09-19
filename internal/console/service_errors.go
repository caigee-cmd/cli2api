package console

import (
	"errors"
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"net/http"
)

func writeOperationError(w http.ResponseWriter, err error) {
	if errors.Is(err, accounts.ErrAccountNotFound) {
		writeErr(w, http.StatusNotFound, "account_not_found", err.Error())
		return
	}
	code := "operation_failed"
	status := http.StatusInternalServerError
	var op *control.OperationError
	var action *providers.ActionError
	if errors.As(err, &action) {
		op = &control.OperationError{Code: action.Code, Err: action.Err}
	}
	if op != nil || errors.As(err, &op) {
		code = op.Code
		switch code {
		case "invalid_request", "invalid_routing_strategy", "invalid_workbuddy_checkin_time", "invalid_checkin_time", "invalid_proxy_url", "invalid_credential", "unsupported_format", "invalid_user_blob", "account_import_failed", "provider_unsupported", "login_start_failed", "login_poll_failed", "login_callback_failed", "model_setting_failed":
			status = http.StatusBadRequest
		case "method_not_allowed":
			status = http.StatusMethodNotAllowed
		case "not_found":
			status = http.StatusNotFound
		case "account_not_running":
			status = http.StatusConflict
		case "not_ready":
			status = http.StatusServiceUnavailable
		case "worker_unavailable", "credential_sync_failed":
			status = http.StatusBadGateway
		}
	}
	writeErr(w, status, code, err.Error())
}
