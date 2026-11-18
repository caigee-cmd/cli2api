package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

func (a *Accounts) login(ctx context.Context, id string) (providers.LoginSessionProvider, error) {
	account, err := a.GetStored(ctx, id)
	if err != nil {
		return nil, err
	}
	adapter, ok := a.Providers.Get(account.Provider)
	if !ok || adapter.Login == nil {
		return nil, operationError("provider_unsupported", "provider does not support this action")
	}
	return adapter.Login, nil
}

// prepareLogin records a provider-specific login choice before the browser
// round trip. Codex picks between its CLI and ChatGPT flows; Kiro picks which
// of its sign-in flows to run. An empty method keeps the provider's default.
func (a *Accounts) prepareLogin(ctx context.Context, account accounts.Account, method, startURL string) error {
	method = strings.TrimSpace(method)
	if method == "" {
		return nil
	}
	var secret, accepted string
	switch account.Provider {
	case "codex":
		secret = "codex_login_method:"
		accepted = "cli or chatgpt"
		switch strings.ToLower(method) {
		case "cli", "chatgpt":
		default:
			return operationError("invalid_request", "codex login method must be "+accepted)
		}
	case "kiro":
		secret = "kiro_login_method:"
		accepted = "builder_id, idc, sso or microsoft"
		switch strings.ToLower(method) {
		case "builder_id", "idc", "sso", "microsoft":
		default:
			return operationError("invalid_request", "kiro login method must be "+accepted)
		}
	default:
		return nil
	}
	if err := a.store().SetSecret(ctx, secret+account.ID, strings.ToLower(method)); err != nil {
		return operationError("login_start_failed", err.Error())
	}
	// Identity Center has no well-known portal, so the operator names it.
	// The URL is checked here because it is later sent the refresh token's
	// client secret during registration.
	if account.Provider == "kiro" && strings.EqualFold(method, "idc") {
		if err := validateIDCStartURL(startURL); err != nil {
			return operationError("invalid_request", err.Error())
		}
		if err := a.store().SetSecret(ctx, "kiro_idc_start_url:"+account.ID, strings.TrimSpace(startURL)); err != nil {
			return operationError("login_start_failed", err.Error())
		}
	}
	return nil
}

// validateIDCStartURL accepts only an https AWS access portal address. The
// start URL becomes the OIDC issuer, so a arbitrary host would receive the
// client registration.
func validateIDCStartURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" {
		return fmt.Errorf("identity center start url must be an https url")
	}
	host := strings.ToLower(parsed.Hostname())
	if !strings.HasSuffix(host, ".awsapps.com") || !strings.HasPrefix(parsed.EscapedPath(), "/start") {
		return fmt.Errorf("identity center start url must be an awsapps.com portal")
	}
	return nil
}

func (a *Accounts) StartLogin(ctx context.Context, id string) (providers.LoginSession, error) {
	login, err := a.login(ctx, id)
	if err != nil {
		return providers.LoginSession{}, err
	}
	session, err := login.StartLogin(ctx, id)
	if err != nil {
		return session, operationError("login_start_failed", err.Error())
	}
	return session, nil
}
func (a *Accounts) PollLogin(ctx context.Context, id string) (bool, string, error) {
	login, err := a.login(ctx, id)
	if err != nil {
		return false, "", err
	}
	done, message, err := login.PollLogin(ctx, id)
	if err != nil {
		return done, message, operationError("login_poll_failed", err.Error())
	}
	return done, message, nil
}
func (a *Accounts) CompleteLogin(ctx context.Context, id, callback string) error {
	login, err := a.login(ctx, id)
	if err != nil {
		return err
	}
	completer, ok := login.(providers.LoginCompleter)
	if !ok {
		return operationError("provider_unsupported", "provider does not accept a pasted callback URL")
	}
	if err := completer.CompleteLogin(ctx, id, callback); err != nil {
		return operationError("login_callback_failed", err.Error())
	}
	return nil
}

// LoginPAT stores a pasted provider-native token for an in-process account whose
// adapter exposes a PAT credential (for example Command Code's user_… key). The
// Qoder child runtime keeps its worker login path; only adapters that implement
// CredentialImporter accept this. The token is wrapped as {"api_key": …} for the
// importer, then persisted in the provider's own credential format and the
// account is enabled and started.
func (a *Accounts) LoginPAT(ctx context.Context, id, token string) error {
	account, err := a.GetStored(ctx, id)
	if err != nil {
		return err
	}
	adapter, ok := a.Providers.Get(account.Provider)
	if !ok || adapter.Credential == nil {
		return operationError("provider_unsupported", "provider does not support PAT login")
	}
	importer, ok := adapter.Credential.(providers.CredentialImporter)
	if !ok {
		return operationError("provider_unsupported", "provider does not support PAT login")
	}
	payload, err := json.Marshal(map[string]string{"api_key": token})
	if err != nil {
		return operationError("invalid_credential", err.Error())
	}
	prepared, err := importer.PrepareImport(payload)
	if err != nil {
		return operationError("invalid_credential", err.Error())
	}
	if err := a.store().SaveCredentialPayload(ctx, id, importer.Format(), prepared.Payload); err != nil {
		return operationError("credential_save_failed", err.Error())
	}
	// Record the credential kind so the card shows the right type (and enables
	// export/filtering) instead of the create-time default "none".
	if err := a.store().SetCredentialAuthType(ctx, id, "pat"); err != nil {
		return operationError("credential_save_failed", err.Error())
	}
	enabled := true
	if err := a.store().Update(ctx, id, accounts.UpdateAccount{Enabled: &enabled}); err != nil {
		return err
	}
	updated, err := a.store().Get(ctx, id)
	if err != nil {
		return err
	}
	if err := a.runtime.StartAccount(ctx, updated); err != nil {
		return err
	}
	return nil
}
