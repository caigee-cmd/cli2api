package workbuddy

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
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
func setCLIChannelHeaders(header http.Header) {
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
	header.Set("X-Private-Data", "false")
}

func newCLIRequestID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "cli2api"
	}
	return hex.EncodeToString(raw[:])
}

// SetChatHeaders carries access identity but never the refresh token.
func SetChatHeaders(header http.Header, credential Credential) {
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
	setCLIChannelHeaders(header)
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
