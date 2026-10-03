package orcarouter

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// PKCE (RFC 7636). The verifier never leaves this process until the exchange and
// is never logged, printed, or placed on a URL; only its S256 challenge travels
// on the authorize URL. Everything here is Go standard library.

// pkceVerifier returns a fresh high-entropy code verifier.
func pkceVerifier() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// pkceChallenge derives base64url(sha256(verifier)) with no padding.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// pkcePair returns a fresh verifier and its S256 challenge. Fresh randomness per
// attempt is mandatory: a reused or guessable verifier breaks the flow's only
// protection.
func pkcePair() (verifier, challenge string, err error) {
	verifier, err = pkceVerifier()
	if err != nil {
		return "", "", err
	}
	if verifier == "" {
		return "", "", errors.New("empty pkce verifier")
	}
	return verifier, pkceChallenge(verifier), nil
}

// newState returns an opaque CSRF token echoed back verbatim by the consent screen.
func newState() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// statesMatch compares the state we sent with the state that came back in
// constant time, so a mismatch cannot be probed byte by byte.
func statesMatch(want, got string) bool {
	if want == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// buildAuthorizeURL assembles the consent URL. callbackURL is either the
// loopback http://127.0.0.1:<port>/cb the listener owns or the literal "oob".
// S256 is always sent: a user may choose "show me a code" on the consent screen
// even for a loopback flow, and a displayed code must be redeemable only by the
// process holding the verifier.
func buildAuthorizeURL(authBase, callbackURL, challenge, state string) (string, error) {
	if strings.TrimSpace(challenge) == "" {
		return "", errors.New("missing code challenge")
	}
	if strings.TrimSpace(state) == "" {
		return "", errors.New("missing state")
	}
	base, err := url.Parse(strings.TrimRight(authBase, "/") + AuthorizePath)
	if err != nil {
		return "", err
	}
	query := base.Query()
	query.Set("callback_url", callbackURL)
	query.Set("code_challenge", challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("app_name", AppName)
	query.Set("scope", ScopeAPI)
	base.RawQuery = query.Encode()
	return base.String(), nil
}

// exchangeURL is the code-for-key endpoint on the authentication origin. It is
// deliberately never "https://api.orcarouter.ai/v1/auth/keys".
func exchangeURL(authBase string) string {
	return strings.TrimRight(authBase, "/") + ExchangePath
}

// loopbackCallbackURL renders the redirect target for a bound listener port.
func loopbackCallbackURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s", port, CallbackPath)
}

// oauthError is one RFC 6749 §5.2 error body.
type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
}

func (e oauthError) message() string {
	switch {
	case e.Error == "" && e.Description == "":
		return ""
	case e.Description == "":
		return e.Error
	case e.Error == "":
		return e.Description
	default:
		return e.Error + ": " + e.Description
	}
}
