package minimaxcode

import (
	"context"
	"fmt"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

const loginStateTTL = deviceLoginTimeout + 5*time.Minute

// StartLogin opens an OAuth device-authorization round. The verification URI
// is returned as AuthURL and the user code is surfaced through PollLogin
// messages, matching the existing device-flow providers.
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	siteID := DefaultSite
	accountBase := sites[siteID].Account
	agentBase := sites[siteID].Agent
	if credential, err := c.loadCredential(ctx, accountID); err == nil {
		if credential.Site != "" {
			siteID = credential.Site
		}
		accountBase = credential.accountBase()
		agentBase = credential.agentBase()
	}
	pair, err := newPKCE()
	if err != nil {
		return providers.LoginSession{}, err
	}
	auth, err := c.requestDeviceCode(ctx, accountBase, pair)
	if err != nil {
		return providers.LoginSession{}, err
	}
	now := c.now()
	c.mu.Lock()
	if c.logins == nil {
		c.logins = map[string]*loginState{}
	}
	for id, pending := range c.logins {
		if now.Sub(pending.expiresAt) > loginStateTTL {
			delete(c.logins, id)
		}
	}
	c.logins[accountID] = &loginState{
		site:        siteID,
		accountBase: accountBase,
		agentBase:   agentBase,
		userCode:    auth.UserCode,
		deviceCode:  auth.DeviceCode,
		verifier:    pair.Verifier,
		verifyURL:   auth.VerifyURI,
		interval:    auth.Interval,
		expiresAt:   now.Add(auth.ExpiresIn),
		pollByUser:  auth.PollByUser,
	}
	c.mu.Unlock()
	return providers.LoginSession{AuthURL: auth.VerifyURI, State: auth.UserCode}, nil
}

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

	result, err := c.pollDeviceToken(ctx, state.accountBase, state.deviceCode, state.verifier, state.pollByUser)
	if err != nil {
		return false, "", err
	}
	switch result.Kind {
	case pollPending:
		message := waitingMessage(state.userCode)
		c.setLoginMessage(accountID, message)
		return false, message, nil
	case pollSlowDown:
		c.backOffLogin(accountID)
		message := waitingMessage(state.userCode)
		c.setLoginMessage(accountID, message)
		return false, message, nil
	case pollDenied:
		c.clearLogin(accountID)
		return false, "", fmt.Errorf("minimaxcode authorization was denied")
	case pollExpired:
		session, startErr := c.StartLogin(ctx, accountID)
		if startErr != nil {
			c.clearLogin(accountID)
			return false, "", startErr
		}
		return false, fmt.Sprintf("the code expired; open %s and enter %s", session.AuthURL, session.State), nil
	case pollFailed:
		return false, "", fmt.Errorf("minimaxcode device poll failed: %s", result.Error)
	}

	credential := Credential{
		Format:      CredentialFormat,
		Site:        state.site,
		AccountBase: state.accountBase,
		AgentBase:   state.agentBase,
		Region:      sites[state.site].Region,
	}
	credential = applyToken(credential, result.Token, c.now())
	if err := c.saveCredential(ctx, accountID, credential); err != nil {
		return false, "", err
	}
	_ = c.store.Observe(ctx, accountID, credential.UserID, "ready", "", "")
	c.clearLogin(accountID)
	return true, "login complete", nil
}

func waitingMessage(userCode string) string {
	if userCode == "" {
		return "waiting for authorization"
	}
	return fmt.Sprintf("waiting for authorization; the code is %s", userCode)
}

func (c *Client) setLoginMessage(accountID, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state := c.logins[accountID]; state != nil {
		state.lastMessage = message
	}
}

func (c *Client) backOffLogin(accountID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.logins[accountID]
	if state == nil {
		return
	}
	state.interval += pollSlowDownStep
	if state.interval > maxPollInterval {
		state.interval = maxPollInterval
	}
}

func (c *Client) clearLogin(accountID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.logins, accountID)
}
