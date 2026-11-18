package kiro

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// useResponses reports whether this account's chat goes to CreateResponse.
// API keys stay on the CLI runtime. Other accounts follow Kiro's
// create_response_api feature flag, cached for fifteen minutes. A read that
// fails before any answer keeps the account on generateAssistantResponse.
func (c *Client) useResponses(ctx context.Context, accountID string, cred Credential) bool {
	if cred.isAPIKey() {
		return false
	}
	return c.readFeatureFlag(ctx, accountID, cred, false)
}

// recheckResponses reads the flag past the cache, at most once per
// featureFlagRecheckGap. A 429 USER_REQUEST_RATE_EXCEEDED from the legacy
// path is what an account Kiro just moved to CreateResponse gets.
func (c *Client) recheckResponses(ctx context.Context, accountID string, cred Credential) bool {
	if cred.isAPIKey() {
		return false
	}
	return c.readFeatureFlag(ctx, accountID, cred, true)
}

type featureFlag struct {
	mu          sync.Mutex
	identity    string
	known       bool
	responses   bool
	failed      bool
	until       time.Time
	recheckedAt time.Time
	call        *featureCall
}

type featureCall struct {
	done      chan struct{}
	responses bool
}

var featureFlags sync.Map

func (c *Client) readFeatureFlag(ctx context.Context, accountID string, cred Credential, recheck bool) bool {
	key := firstNonEmpty(accountID, cred.Email, cred.UserID)
	if key == "" {
		return false
	}
	loaded, _ := featureFlags.LoadOrStore(key, &featureFlag{})
	flag := loaded.(*featureFlag)
	identity := flagIdentity(cred)
	now := time.Now()

	flag.mu.Lock()
	if flag.identity != identity {
		flag.identity = identity
		flag.until = time.Time{}
	}
	var call *featureCall
	switch {
	case recheck:
		if flag.call == nil && !flag.recheckedAt.IsZero() && now.Sub(flag.recheckedAt) < featureFlagRecheckGap {
			responses := flag.known && flag.responses
			flag.mu.Unlock()
			return responses
		}
		flag.recheckedAt = now
		call = c.startFlag(flag, cred)
	case now.Before(flag.until):
		responses := flag.known && flag.responses
		flag.mu.Unlock()
		return responses
	case flag.known:
		c.startFlag(flag, cred)
		responses := flag.responses
		flag.mu.Unlock()
		return responses
	case flag.failed:
		c.startFlag(flag, cred)
		flag.mu.Unlock()
		return false
	default:
		call = c.startFlag(flag, cred)
	}
	flag.mu.Unlock()

	select {
	case <-call.done:
		return call.responses
	case <-ctx.Done():
		return false
	}
}

func (c *Client) startFlag(flag *featureFlag, cred Credential) *featureCall {
	if flag.call != nil {
		return flag.call
	}
	call := &featureCall{done: make(chan struct{})}
	flag.call = call
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), featureFlagTimeout)
		defer cancel()
		responses, err := c.fetchFeatureFlag(ctx, cred)
		flag.finish(call, responses, err)
	}()
	return call
}

func (f *featureFlag) finish(call *featureCall, responses bool, err error) {
	now := time.Now()
	f.mu.Lock()
	if f.call == call {
		f.call = nil
	}
	if err != nil {
		f.until = now.Add(featureFlagRetry)
		f.failed = !f.known
	} else {
		f.known = true
		f.failed = false
		f.responses = responses
		f.until = now.Add(featureFlagTTL)
	}
	call.responses = f.known && f.responses
	f.mu.Unlock()
	close(call.done)
}

func flagIdentity(cred Credential) string {
	sum := sha256.Sum256([]byte(cred.bearer() + "\x00" + cred.requestProfileArn()))
	return hex.EncodeToString(sum[:8])
}

func (c *Client) fetchFeatureFlag(ctx context.Context, cred Credential) (bool, error) {
	profile := cred.requestProfileArn()
	host := runtimeHost(cred.dataPlaneRegion())
	endpoint := chatBaseFor(cred) + "/featureConfiguration?origin=AI_EDITOR&version=" + currentIDEVersion()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	applyRuntimeHeaders(req, cred, host)
	if profile != "" {
		req.Header.Set("x-amzn-kiro-profile-arn", profile)
	}
	client, err := c.plainClient()
	if err != nil {
		return false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	noteVersionGate(client, string(body))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("feature configuration HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Configuration map[string]json.RawMessage `json:"configuration"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return false, err
	}
	raw, ok := parsed.Configuration[featureWireKey("create_response_api")]
	if !ok {
		return false, nil
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return false, nil
	}
	return value == createResponseFlag, nil
}

func featureWireKey(name string) string {
	sum := sha256.Sum256([]byte(featureConfigSalt + name))
	return hex.EncodeToString(sum[:])
}

func (c *Client) plainClient() (*http.Client, error) {
	client := *c.http
	return &client, nil
}

func applyRuntimeHeaders(req *http.Request, cred Credential, host string) {
	if token := cred.bearer(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if kind := cred.tokenType(); kind != "" {
		req.Header.Set("tokentype", kind)
	}
	req.Header.Set("User-Agent", sdkUserAgent(cred, "codewhispererruntime", RuntimeSDKVersion, "m/N,E"))
	req.Header.Set("x-amz-user-agent", amzUserAgent(cred, RuntimeSDKVersion))
	req.Header.Set("x-amzn-codewhisperer-optout", "true")
	req.Header.Set("Accept", "application/json")
	if host != "" {
		req.Host = host
	}
}

func sdkUserAgent(cred Credential, apiName, sdkVersion, mode string) string {
	agent := fmt.Sprintf("aws-sdk-js/%s ua/2.1 os/%s lang/js md/nodejs#%s api/%s#%s %s KiroIDE-%s",
		sdkVersion, systemFingerprint(), nodeVersion, apiName, sdkVersion, mode, currentIDEVersion())
	if cred.MachineID != "" {
		agent += "-" + cred.MachineID
	}
	return agent
}

func amzUserAgent(cred Credential, sdkVersion string) string {
	agent := fmt.Sprintf("aws-sdk-js/%s KiroIDE-%s", sdkVersion, currentIDEVersion())
	if cred.MachineID != "" {
		agent += "-" + cred.MachineID
	}
	return agent
}

func applyResponsesHeaders(req *http.Request, cred Credential, attempt int) {
	if token := cred.bearer(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if cred.isExternal() {
		req.Header.Set("tokentype", "EXTERNAL_IDP")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", responsesUserAgent(cred))
	req.Header.Set("x-amzn-kiro-origin", Origin)
	req.Header.Set("x-amzn-codewhisperer-optout", "true")
	req.Header.Set("x-amzn-kiro-attempt", fmt.Sprintf("%d;max=%d", attempt, ResponsesAttempts))
	if profile := cred.requestProfileArn(); profile != "" {
		req.Header.Set("x-amzn-kiro-profile", profile)
	}
}

func responsesUserAgent(cred Credential) string {
	parts := []string{"KiroIDE", currentIDEVersion()}
	if cred.MachineID != "" {
		parts = append(parts, cred.MachineID)
	}
	return strings.Join(parts, " ") + " KAS/" + AgentVersion + " acp-client/kiro-ide"
}
