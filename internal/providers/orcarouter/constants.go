// Package orcarouter implements the OrcaRouter in-process provider.
//
// OrcaRouter is an OpenAI-compatible AI gateway: one endpoint in front of many
// vendors, addressed by "<vendor>/<model>" ids. Two authentication entries share
// one credential seam:
//
//   - "orcarouter"       — paste an existing sk-orca-… key.
//   - "orcarouter-oauth" — OAuth 2.0 + PKCE browser consent on the auth origin,
//     which mints the same kind of sk-orca-… key.
//
// The two public origins are deliberately distinct and never derived from one
// another: authentication lives on www.orcarouter.ai (/auth and
// /api/v1/auth/keys) while inference and the model catalog live on
// api.orcarouter.ai/v1. https://api.orcarouter.ai/v1/auth/keys is a 404.
package orcarouter

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const (
	// ProviderID selects OrcaRouter with a pasted API key.
	ProviderID = "orcarouter"
	// OAuthProviderID selects OrcaRouter via OAuth 2.0 + PKCE browser consent.
	OAuthProviderID = "orcarouter-oauth"

	// CredentialFormat is the stored payload format for the pasted-key entry.
	CredentialFormat = "orcarouter-key-v1"
	// CredentialFormatOAuth is the stored payload format for the PKCE entry.
	// The payload shape is identical; the format records which entry minted it.
	CredentialFormatOAuth = "orcarouter-oauth-v1"

	// DefaultAuthBase hosts the consent screen and the code exchange.
	DefaultAuthBase = "https://www.orcarouter.ai"
	// DefaultAPIBase is the inference/model-catalog base, including /v1.
	DefaultAPIBase = "https://api.orcarouter.ai/v1"

	// AuthorizePath is fixed by the consent endpoint.
	AuthorizePath = "/auth"
	// ExchangePath is the code-for-key exchange. It is NOT under /v1.
	ExchangePath = "/api/v1/auth/keys"
	// IdentityPath returns the account behind a key (no ID token is issued).
	IdentityPath = "/api/user/self"

	// ModelsPath and ChatPath are relative to the inference base.
	ModelsPath = "/models"
	ChatPath   = "/chat/completions"

	// KeyPrefix is the shape of an OrcaRouter API key.
	KeyPrefix = "sk-orca-"

	// AppName is the label the consent screen shows as a claim.
	AppName = "CLI2API"

	// ScopeAPI is the only scope this client requests.
	ScopeAPI = "api"

	// PromptConsent forces re-approval even after an earlier approval.
	PromptConsent = "consent"

	// CallbackPath is the loopback path the consent screen redirects to.
	CallbackPath = "/cb"

	// CallbackOOB is the literal callback_url that selects out-of-band delivery.
	CallbackOOB = "oob"

	// ClientSecretParam is only referenced by the redactor's regression test; a
	// client secret is never sent, accepted, or stored.
	ClientSecretParam = "client_secret"
)

// Env names for the shared and per-origin overrides. Explicit overrides win
// over the shared base; the shared base wins over the public defaults.
const (
	AuthBaseEnv = "ORCA_AUTH_BASE_URL"
	APIBaseEnv  = "ORCA_API_BASE_URL"
	SharedEnv   = "ORCA_BASE_URL"

	// SecretSharedBase stores a self-hosted shared origin in app_secrets.
	SecretSharedBase = "orcarouter_base_url"
	SecretAuthBase   = "orcarouter_auth_base_url"
	SecretAPIBase    = "orcarouter_api_base_url"
)

// Origins is the resolved pair of public origins.
type Origins struct {
	AuthBase string
	APIBase  string
}

// ResolveOrigins applies explicit auth/API overrides first, then a shared
// self-hosted base, then the public defaults. One origin is never derived from
// the other by rewriting a hostname or blind "/v1" concatenation.
func ResolveOrigins(shared, authOverride, apiOverride string) (Origins, error) {
	auth := strings.TrimSpace(authOverride)
	if auth == "" {
		auth = strings.TrimSpace(shared)
	}
	if auth == "" {
		auth = DefaultAuthBase
	}
	api := strings.TrimSpace(apiOverride)
	if api == "" {
		api = strings.TrimSpace(shared)
	}
	if api == "" {
		api = DefaultAPIBase
	}
	// Local development defaults for a self-hosted shared base (a bare
	// localhost:port) are expanded before validation so the operator does not
	// have to spell out each path.
	if expanded, ok := expandLoopbackPair(auth, api); ok {
		auth, api = expanded[0], expanded[1]
	}
	normalizedAuth, err := normalizeOrigin(auth, "")
	if err != nil {
		return Origins{}, fmt.Errorf("orcarouter auth origin: %w", err)
	}
	normalizedAPI, err := normalizeOrigin(api, "/v1")
	if err != nil {
		return Origins{}, fmt.Errorf("orcarouter api origin: %w", err)
	}
	return Origins{AuthBase: normalizedAuth, APIBase: normalizedAPI}, nil
}

// expandLoopbackPair expands a shared localhost/loopback value (with or without
// a scheme) into a concrete auth origin and a concrete /v1 API origin. It is the
// documented self-hosted development convenience: one local base covers both
// origins without either being derived from the other by hostname surgery.
func expandLoopbackPair(auth, api string) ([2]string, bool) {
	candidate, ok := sharedLoopbackCandidate(auth, api)
	if !ok {
		return [2]string{}, false
	}
	host, port, err := net.SplitHostPort(candidate)
	if err != nil || !IsLoopbackHost(host) || port == "" {
		return [2]string{}, false
	}
	base := "http://" + net.JoinHostPort(host, port)
	return [2]string{base, base + "/v1"}, true
}

func sharedLoopbackCandidate(auth, api string) (string, bool) {
	if strings.TrimSpace(auth) != strings.TrimSpace(api) {
		return "", false
	}
	candidate := strings.TrimSpace(auth)
	candidate = strings.TrimPrefix(candidate, "http://")
	candidate = strings.TrimPrefix(candidate, "https://")
	candidate = strings.TrimSuffix(candidate, "/")
	if strings.Contains(candidate, "/") {
		return "", false
	}
	if !strings.Contains(candidate, ":") {
		return "", false
	}
	return candidate, true
}

// normalizeOrigin validates one configured origin. Remote origins must be
// HTTPS; plain HTTP is allowed only for loopback development. defaultPath is
// appended when the configured value carries no path of its own.
func normalizeOrigin(raw, defaultPath string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty origin")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", err
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("origin %q has no host", trimmed)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "https":
	case "http":
		if !IsLoopbackHost(parsed.Hostname()) {
			return "", fmt.Errorf("origin %q must use https unless it is loopback", trimmed)
		}
	default:
		return "", fmt.Errorf("origin %q must be http or https", trimmed)
	}
	path := strings.TrimRight(parsed.Path, "/")
	if path == "" {
		path = defaultPath
	}
	parsed.Scheme = scheme
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

// IsLoopbackHost reports whether a host is a loopback literal or localhost.
func IsLoopbackHost(host string) bool {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]")) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
