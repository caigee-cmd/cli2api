package kimi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// loginStateTTL bounds how long an abandoned device round stays in memory.
const loginStateTTL = deviceLoginTimeout + 5*time.Minute

// StartLogin opens a device-authorization round and returns the URL the user
// must open. Kimi's device grant needs no loopback listener, so the console's
// generic browser tab drives it end to end.
func (c *Client) StartLogin(ctx context.Context, accountID string) (providers.LoginSession, error) {
	authHost := c.authHost
	if authHost == "" {
		authHost = CNOAuthHost
	}
	deviceID := ""
	if credential, err := c.loadCredential(ctx, accountID); err == nil {
		deviceID = credential.DeviceID
		if host := credential.AuthHost(); host != "" {
			authHost = host
		}
	}
	if deviceID == "" {
		deviceID = newDeviceID()
	}
	authorization, err := c.requestDeviceCode(ctx, authHost, deviceID)
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
		authHost:   authHost,
		deviceID:   deviceID,
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

// PollLogin advances the device round by one poll. The console calls it on a
// short interval, so it throttles itself to the server-declared cadence and
// keeps reporting the user code while the browser round is still pending.
func (c *Client) PollLogin(ctx context.Context, accountID string) (bool, string, error) {
	c.mu.Lock()
	state := c.logins[accountID]
	c.mu.Unlock()
	if state == nil {
		return false, "", fmt.Errorf("login not started for account %s", accountID)
	}
	now := c.now()
	if now.After(state.expiresAt) {
		// The code expired before the user finished; mint a fresh one so the
		// console's poll loop keeps working instead of dead-ending.
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

	result, err := c.pollDeviceToken(ctx, state.authHost, state.deviceCode, state.deviceID)
	if err != nil {
		return false, "", err
	}
	switch result.Kind {
	case devicePollPending:
		message := waitingMessage(state.userCode)
		c.setLoginMessage(accountID, message)
		return false, message, nil
	case devicePollSlowDown:
		// RFC 8628 §3.5: widen the interval so the next PollLogin waits longer
		// before it asks the token endpoint again.
		c.backOffLogin(accountID)
		message := waitingMessage(state.userCode)
		c.setLoginMessage(accountID, message)
		return false, message, nil
	case devicePollDenied:
		c.clearLogin(accountID)
		return false, "", fmt.Errorf("kimi authorization was denied")
	case devicePollExpired:
		session, startErr := c.StartLogin(ctx, accountID)
		if startErr != nil {
			c.clearLogin(accountID)
			return false, "", startErr
		}
		return false, fmt.Sprintf("the code expired; open %s and enter %s", session.AuthURL, session.State), nil
	}

	credential := applyToken(Credential{
		Format:    CredentialFormat,
		DeviceID:  state.deviceID,
		OAuthHost: state.authHost,
	}, result.Token, c.now())
	if profile, profileErr := c.fetchProfileWithToken(ctx, credential); profileErr == nil {
		credential.UserID = profile.UserID
		credential.UserName = profile.UserName
		credential.Plan = profile.Plan
	}
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

// fetchProfileWithToken reads /me with a credential that is not stored yet, so
// a fresh login can be labeled before the account is probed.
func (c *Client) fetchProfileWithToken(ctx context.Context, credential Credential) (profileInfo, error) {
	base := c.apiBase(credential)
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, base+mePath, nil)
	if err != nil {
		return profileInfo{}, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	req.Header.Set("Accept", "application/json")
	client, err := c.httpClient(ctx, "")
	if err != nil {
		return profileInfo{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return profileInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return profileInfo{}, fmt.Errorf("kimi profile request failed (HTTP %d)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return profileInfo{}, err
	}
	return ParseProfileJSON(body)
}

func (c *Client) setLoginMessage(accountID, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state := c.logins[accountID]; state != nil {
		state.lastMessage = message
	}
}

// backOffLogin widens the device-token poll interval after a slow_down, as
// RFC 8628 asks. PollLogin already throttles on state.interval, so the next
// console poll waits the new interval instead of hammering the token endpoint.
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
