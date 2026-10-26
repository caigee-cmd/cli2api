package kimi

import "time"

// ProviderID is the provider family id registered in the control plane. Kimi
// Code is one family; the China region is the only region this adapter serves.
const ProviderID = "kimi"

// CredentialFormat is the canonical stored credential format.
const CredentialFormat = "kimi-oauth-v1"

// ClientID is the public Kimi Code OAuth client. The Kimi Code CLI, the Kimi
// Code desktop app and the older kimi-cli all authorize with this same
// device-grant client, so a token minted for the CLI is accepted here.
const ClientID = "17e5f671-d194-4dfb-9706-5516cb48c098"

// China-region endpoints. The managed Kimi Code API is versioned under
// /coding/v1 and is distinct from the Moonshot Open Platform (api.moonshot.cn)
// API-key universe.
const (
	CNOAuthHost = "https://auth.kimi.com"
	CNBaseURL   = "https://api.kimi.com/coding/v1"
)

// OAuth device-grant paths, relative to the OAuth host.
const (
	DeviceAuthorizationPath = "/api/oauth/device_authorization"
	TokenPath               = "/api/oauth/token"
	// DeviceGrantType is the RFC 8628 device-code grant.
	DeviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
)

// Client identity reported to the OAuth host and the managed API. Kimi's
// clients send this family of headers on chat and model requests; the value
// mirrors the installed Kimi Code client so the gateway is indistinguishable
// from the official CLI on the wire.
const (
	Platform      = "kimi_code_cli"
	ClientVersion = "1.1.2"
	UserAgent     = "kimi-code-cli/" + ClientVersion
)

// Managed API paths, relative to CNBaseURL.
const (
	modelsPath = "/models"
	chatPath   = "/chat/completions"
	usagesPath = "/usages"
	mePath     = "/me"
)

// Timing knobs.
const (
	// refreshLead refreshes the access token this long before it expires, so a
	// long stream never starts on a token that dies mid-response.
	refreshLead = 5 * time.Minute
	// deviceLoginTimeout bounds one device-code round. Kimi issues a 30-minute
	// code; the local cap keeps a forgotten login from pinning a poll loop.
	deviceLoginTimeout = 15 * time.Minute
	// pollFloor is the minimum device-token poll interval.
	pollFloor = 5 * time.Second
	// pollSlowDownStep is added after the server answers slow_down (RFC 8628).
	pollSlowDownStep = 5 * time.Second
	// maxPollInterval caps the slow_down back-off so a device code that is
	// still pending when the user is slow can never outlive its own window.
	maxPollInterval = 60 * time.Second
	// catalogTTL caps how long a discovered model list is reused.
	catalogTTL = 10 * time.Minute
	// metadataTimeout bounds the non-stream metadata calls (models/me/usages).
	metadataTimeout = 20 * time.Second
)
