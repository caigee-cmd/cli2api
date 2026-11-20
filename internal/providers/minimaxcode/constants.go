// Package minimaxcode implements an in-process adapter for the MiniMax Code
// subscription product (agent.minimax.cn / agent.minimax.io). It is distinct
// from the MiniMax Open Platform API-key gateway. Authentication uses the
// official OAuth 2.0 device flow (public client mcode-public, PKCE S256) and
// chat uses the Anthropic Messages API the Code CLI talks to.
package minimaxcode

import "time"

const (
	ProviderID       = "minimaxcode"
	CredentialFormat = "minimaxcode-oauth-v1"

	ClientID = "mcode-public"
	Scope    = "agent.default"
	Audience = "agent-backend"
	// DeviceGrantType is the RFC 8628 device-code grant.
	DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	AgentID          = "main"
	UserAgent        = "MiniMaxAgent"
	AnthropicVersion = "2023-06-01"
)

type siteConfig struct {
	Account string
	Agent   string
	Region  string
}

var sites = map[string]siteConfig{
	"cn":     {Account: "https://account.minimax.cn", Agent: "https://agent.minimax.cn", Region: "cn"},
	"global": {Account: "https://account.minimax.io", Agent: "https://agent.minimax.io", Region: "global"},
}

const DefaultSite = "cn"

const (
	deviceCodePath = "/oauth2/device/code"
	tokenPath      = "/oauth2/token"
	messagesPath   = "/mavis/api/v1/llm/v1/messages"
	modelsPath     = "/mavis/api/v1/models"
)

const (
	refreshLead        = 2 * time.Minute
	deviceLoginTimeout = 15 * time.Minute
	pollFloor          = 5 * time.Second
	pollSlowDownStep   = 5 * time.Second
	maxPollInterval    = 60 * time.Second
	catalogTTL         = 10 * time.Minute
	metadataTimeout    = 20 * time.Second
)

// knownModels is the static fallback catalog mirroring the Code CLI registry.
var knownModels = []string{
	"MiniMax-M3.1-Flash-Preview",
	"MiniMax-M3",
	"MiniMax-M2.7",
	"MiniMax-M2.7-highspeed",
}

const defaultModel = "MiniMax-M3"
