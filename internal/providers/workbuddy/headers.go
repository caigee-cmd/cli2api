package workbuddy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"time"
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

// setCLIChannelHeaders is chat-only. Values come from the official
// CodeBuddy CLI (craft / CLI / SaaS, plus the OpenAI Node SDK fingerprint
// that package sends). CN and Global share the names; Origin/host stay split.
//
// The official CLI keeps X-Conversation-ID for the life of a session
// (its session.id) and mints X-Request-ID fresh per request, equal to
// X-Conversation-Message-ID. A new conversation id on every call turns one
// chat into thousands of one-shot sessions. With a session key we derive the
// conversation id from the account and that key, so the turns of one chat
// share it and two accounts never share it. Without a key the id stays
// per-request, matching the official client when it has no session.
func setCLIChannelHeaders(header http.Header, credential Credential, conversation string) {
	conversationID := conversationIDFor(credential.UID, conversation)
	messageID := newCLIMessageID()
	header.Set("X-Request-ID", messageID)
	header.Set("X-Conversation-ID", conversationID)
	header.Set("X-Conversation-Request-ID", conversationID)
	header.Set("X-Root-Request-ID", conversationID)
	header.Set("X-Conversation-Message-ID", messageID)
	header.Set("X-Agent-Type", agentTypeMain)
	header.Set("X-Agent-Intent", agentIntentDefault)
	header.Set("X-Agent-Purpose", agentPurposeConversation)
	header.Set("X-Agent-Runtime", agentRuntimeOther)
	header.Set("X-IDE-Type", productTypeCLI)
	header.Set("X-IDE-Name", productTypeCLI)
	header.Set("X-IDE-Version", CLIVersion)
	setCLIStainlessHeaders(header)
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

// setCLIStainlessHeaders adds the OpenAI Node SDK fingerprint the official
// CLI attaches to chat. Names are lowercase, matching the SDK's wire form.
func setCLIStainlessHeaders(header http.Header) {
	header.Set("X-Stainless-Lang", CLIStainlessLang)
	header.Set("X-Stainless-Package-Version", CLIStainlessPackageVersion)
	header.Set("X-Stainless-OS", CLIStainlessOS)
	header.Set("X-Stainless-Arch", CLIStainlessArch)
	header.Set("X-Stainless-Runtime", CLIStainlessRuntime)
	header.Set("X-Stainless-Runtime-Version", CLIStainlessRuntimeVersion)
	header.Set("X-Stainless-Retry-Count", CLIStainlessRetryCount)
	header.Set("X-Stainless-Timeout", CLIStainlessTimeout)
}

// conversationIDFor derives the dashed UUID the official CLI reuses as
// X-Conversation-ID for one account and one session. An empty session has no
// stable id, so the caller falls back to a fresh one.
func conversationIDFor(uid, session string) string {
	session = strings.TrimSpace(session)
	if session == "" {
		return newCLIConversationID()
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(uid) + "|" + session))
	raw := sum[:16]
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

// newCLIConversationID is a dashed UUIDv7, the shape captured on the official
// client's conversation id.
func newCLIConversationID() string {
	raw := newUUIDv7()
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}

// newCLIMessageID is 32 hex without dashes, the shape captured for
// X-Request-ID and X-Conversation-Message-ID.
func newCLIMessageID() string {
	raw := newUUIDv7()
	return hex.EncodeToString(raw[:])
}

func newUUIDv7() [16]byte {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return raw
	}
	ms := uint64(time.Now().UnixMilli())
	raw[0] = byte(ms >> 40)
	raw[1] = byte(ms >> 32)
	raw[2] = byte(ms >> 24)
	raw[3] = byte(ms >> 16)
	raw[4] = byte(ms >> 8)
	raw[5] = byte(ms)
	raw[6] = (raw[6] & 0x0f) | 0x70
	raw[8] = (raw[8] & 0x3f) | 0x80
	return raw
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
// conversation is the gateway session key; empty keeps a fresh conversation
// id, since the official client does the same when it has no session.
func SetChatHeaders(header http.Header, credential Credential, conversation string) {
	setIdentityHeaders(header, credential)
	setCLIChannelHeaders(header, credential, conversation)
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
