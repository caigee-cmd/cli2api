package control

import (
	"context"
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
