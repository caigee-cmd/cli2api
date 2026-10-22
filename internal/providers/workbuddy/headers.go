package workbuddy

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
)

func setCommonHeaders(header http.Header, global bool) {
	origin := "https://www.codebuddy.cn"
	if global {
		origin = "https://www.workbuddy.ai"
	}
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json, text/plain, */*")
	header.Set("X-Requested-With", "XMLHttpRequest")
	header.Set("Origin", origin)
	header.Set("Referer", origin+"/")
	header.Set("User-Agent", UserAgent)
}

// setCLIChannelHeaders is chat-only. Values come from CodeBuddy CLI 2.139.0
// (craft / CLI / SaaS). CN and Global share the names; Origin/host stay split.
func setCLIChannelHeaders(header http.Header, credential Credential) {
	requestID := newCLIRequestID()
	header.Set("X-Request-ID", requestID)
	header.Set("X-Conversation-ID", requestID)
	header.Set("X-Session-ID", requestID)
	header.Set("X-Conversation-Request-ID", requestID)
	header.Set("X-Conversation-Message-ID", requestID)
	header.Set("X-Agent-Type", agentTypeMain)
	header.Set("X-Agent-Intent", agentIntentDefault)
	header.Set("X-IDE-Type", productTypeCLI)
	header.Set("X-IDE-Name", productTypeCLI)
	header.Set("X-IDE-Version", CLIVersion)
	header.Set("X-Product-Version", CLIVersion)
	if value, ok := privateDataHeader(identityCLI, privateDataModeFromEnv(), credential); ok {
		header.Set("X-Private-Data", value)
	} else {
		header.Del("X-Private-Data")
	}
}

// Identities the X-Private-Data default depends on (workbuddy2api-hub #516).
// cli2api's chat path only speaks the CLI channel today; the others are kept
// so the auto table matches upstream and stays testable.
const (
	identityCLI       = "cli"
	identityWorkBuddy = "workbuddy"
	identityVSCode    = "vscode"
)

// PrivateDataEnv selects the X-Private-Data header: auto (default), true or
// false. Explicit true/false is sent verbatim for every identity, including
// the VSCode China edition that sends no header under auto.
const PrivateDataEnv = "WORKBUDDY_PRIVATE_DATA"

func privateDataModeFromEnv() string {
	return normalizePrivateDataMode(os.Getenv(PrivateDataEnv))
}

func normalizePrivateDataMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "on", "yes":
		return "true"
	case "false", "0", "off", "no":
		return "false"
	default:
		return "auto"
	}
}

// privateDataHeader returns the X-Private-Data value and whether to send it.
// auto mirrors the official clients: CLI sends false for personal and true
// for enterprise accounts; the WorkBuddy desktop always sends true; VSCode
// sends true on the global site and omits the header on the China site.
func privateDataHeader(identity, mode string, credential Credential) (string, bool) {
	switch mode {
	case "true", "false":
		return mode, true
	}
	switch identity {
	case identityWorkBuddy:
		return "true", true
	case identityVSCode:
		if credential.IsGlobal() {
			return "true", true
		}
		return "", false
	default:
		if credential.EnterpriseID != "" {
			return "true", true
		}
		return "false", true
	}
}

func newCLIRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "cli2api"
	}
	return hex.EncodeToString(raw[:])
}

func setIdentityHeaders(header http.Header, credential Credential) {
	setCommonHeaders(header, credential.IsGlobal())
	if credential.AccessToken != "" {
		header.Set("Authorization", "Bearer "+credential.AccessToken)
	} else {
		header.Set("X-No-Authorization", "1")
	}
	if credential.UID != "" {
		header.Set("X-User-Id", credential.UID)
	} else {
		header.Set("X-No-User-Id", "1")
	}
	if credential.EnterpriseID != "" {
		header.Set("X-Enterprise-Id", credential.EnterpriseID)
	} else {
		header.Set("X-No-Enterprise-Id", "1")
	}
	if credential.Domain != "" {
		header.Set("X-Domain", credential.Domain)
	} else {
		header.Set("X-No-Department-Info", "1")
	}
	header.Set("X-Product", "SaaS")
}

// SetCatalogHeaders carries access identity for the product-config catalog
// (/v3/config). Chat-only CLI channel headers stay off this path.
//
// UA is region-gated on purpose:
//   - Global: desktop UA matches the WorkBuddy AI dropdown (CLI UA omits
//     deepseek-v4.1-flash on the same account).
//   - CN: keep the CLI UA. Live A/B showed WorkBuddy desktop UA on CN returns
//     a different, worse set (no glm-5.3-flash / empty cli agent allowlist).
func SetCatalogHeaders(header http.Header, credential Credential) {
	setIdentityHeaders(header, credential)
	if credential.IsGlobal() {
		header.Set("User-Agent", DesktopUserAgent)
	}
}

// SetChatHeaders carries access identity but never the refresh token.
func SetChatHeaders(header http.Header, credential Credential) {
	setIdentityHeaders(header, credential)
	setCLIChannelHeaders(header, credential)
}

// SetRefreshHeaders is the only path allowed to carry X-Refresh-Token.
func SetRefreshHeaders(header http.Header, credential Credential) {
	setCommonHeaders(header, credential.IsGlobal())
	header.Set("X-Refresh-Token", credential.RefreshToken)
	if credential.EnterpriseID != "" {
		header.Set("X-Enterprise-Id", credential.EnterpriseID)
	}
	header.Set("X-Auth-Refresh-Source", "workbuddy")
}

// SetBillingHeaders carries identity for the separate billing hosts.
func SetBillingHeaders(header http.Header, credential Credential) {
	setCommonHeaders(header, credential.IsGlobal())
	if credential.AccessToken != "" {
		header.Set("Authorization", "Bearer "+credential.AccessToken)
	}
	if credential.UID != "" {
		header.Set("X-User-Id", credential.UID)
	}
	if credential.EnterpriseID != "" {
		header.Set("X-Enterprise-Id", credential.EnterpriseID)
		header.Set("X-Tenant-Id", credential.EnterpriseID)
	}
	if credential.Domain != "" {
		header.Set("X-Domain", credential.Domain)
	}
}
