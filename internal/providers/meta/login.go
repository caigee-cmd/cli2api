package meta

import (
	"context"
	"fmt"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

const loginStateTTL = deviceLoginTimeout + 5*time.Minute

type loginState struct {
	userCode    string
	deviceCode  string
	verifyURL   string
	interval    time.Duration
	expiresAt   time.Time
	lastPolled  time.Time
	lastMessage string
}

// StartLogin opens a device-authorization round (no loopback).
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	authorization, err := c.requestDeviceCode(ctx, accountID)
	if err != nil {
		return providers.LoginSession{}, err
	}
	c.mu.Lock()
	if c.logins == nil {
		c.logins = map[string]*loginState{}
	}
	now := c.now()
	for id, pending := range c.logins {
		if now.Sub(pending.expiresAt) > loginStateTTL {
			delete(c.logins, id)
		}
	}
	c.logins[accountID] = &loginState{
		userCode:   authorization.UserCode,
		deviceCode: authorization.DeviceCode,
		verifyURL:  authorization.VerificationURIComplete,
		interval:   authorization.Interval,
		expiresAt:  now.Add(authorization.ExpiresIn),
	}
	c.mu.Unlock()
	return providers.LoginSession{
		AuthURL: authorization.VerificationURIComplete,
		State:   authorization.UserCode,
	}, nil
}

// PollLogin advances one device-token poll; on success mints the API key and
// persists meta-oauth-v1.
func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	state := c.logins[accountID]
	c.mu.Unlock()
	if state == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	now := c.now()
	if now.After(state.expiresAt) {
		session, err := c.StartLogin(ctx, accountID)
		if err != nil {
			c.clearLogin(accountID)
			return false, "", err
		}
		return false, fmt.Sprintf("the code expired; open %s and enter %s", session.AuthURL, session.State), nil
	}
	c.mu.Lock()
	if !state.lastPolled.IsZero() && now.Sub(state.lastPolled) < state.interval {
		message := state.lastMessage
		if message == "" {
			message = waitingMessage(state.userCode)
		}
		c.mu.Unlock()
		return false, message, nil
	}
	state.lastPolled = now
	c.mu.Unlock()

	result, err := c.pollDeviceToken(ctx, accountID, state.deviceCode)
	if err != nil {
		return false, "", err
	}
	switch result.Kind {
	case devicePollPending:
		message := waitingMessage(state.userCode)
		c.setLoginMessage(accountID, message)
		return false, message, nil
	case devicePollSlowDown:
		c.backOffLogin(accountID)
		message := waitingMessage(state.userCode)
		c.setLoginMessage(accountID, message)
		return false, message, nil
	case devicePollDenied:
		c.clearLogin(accountID)
		return false, "", fmt.Errorf("meta authorization was denied")
	case devicePollExpired:
		session, startErr := c.StartLogin(ctx, accountID)
		if startErr != nil {
			c.clearLogin(accountID)
			return false, "", startErr
		}
		return false, fmt.Sprintf("the code expired; open %s and enter %s", session.AuthURL, session.State), nil
	}

	minted, err := c.mintAPIKey(ctx, accountID, result.Token.AccessToken)
	if err != nil {
		return false, "", err
	}
	credential := applyLoginBundle(result.Token, minted, c.now())
	if err := c.saveCredential(ctx, accountID, credential); err != nil {
		return false, "", err
	}
	uid := firstNonEmpty(credential.Email, credential.Name)
	_ = c.store.Observe(ctx, accountID, uid, "ready", "", "")
	c.clearLogin(accountID)
	return true, "login complete", nil
}

func waitingMessage(userCode string) string {
	if userCode == "" {
		return "waiting for authorization"
	}
	return fmt.Sprintf("waiting for authorization; the code is %s", userCode)
}

func (c *Client) clearLogin(accountID string) {
	c.mu.Lock()
	delete(c.logins, accountID)
	c.mu.Unlock()
}

func (c *Client) setLoginMessage(accountID, message string) {
	c.mu.Lock()
	if state := c.logins[accountID]; state != nil {
		state.lastMessage = message
	}
	c.mu.Unlock()
}

func (c *Client) backOffLogin(accountID string) {
	c.mu.Lock()
	if state := c.logins[accountID]; state != nil {
		state.interval += pollSlowDownStep
		if state.interval > maxPollInterval {
			state.interval = maxPollInterval
		}
	}
	c.mu.Unlock()
}
