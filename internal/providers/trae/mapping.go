package trae

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func normalizeReasoningLevel(raw string) string {
	return providers.NormalizeReasoningLevel(raw)
}

func requestedReasoningLevel(req translate.ChatRequest) string {
	return providers.ParseReasoningIntent(req).Level
}

func clampReasoningLevel(level string, caps providers.ModelCapabilities) string {
	return providers.ResolveReasoningLevel(level, caps)
}

func applySoloChatFields(obj map[string]any, req translate.ChatRequest, maxMode bool, storedLevel string, caps providers.ModelCapabilities, uid, session string) {
	if obj == nil {
		return
	}
	for _, key := range []string{
		"enable_thinking", "enable_reasoning", "is_reasoning", "reasoning_effort",
		"reasoning_budget_tokens", "thinking", "context_length", "max_input_tokens",
	} {
		delete(obj, key)
	}
	if maxMode && caps.MaxMode {
		obj["is_max_mode"] = 1
	} else {
		delete(obj, "is_max_mode")
	}
	level := ""
	if providers.RequestsDisabledThinking(req) {
		level = "none"
	} else {
		level = requestedReasoningLevel(req)
		if level == "" {
			level = storedLevel
		}
		level = clampReasoningLevel(level, caps)
	}
	if level == "" || level == "none" {
		delete(obj, "reasoning_effort_level")
	} else {
		obj["reasoning_effort_level"] = level
	}
	// llm_utils_chat carries conversation_id / session_id, and a fresh pair on
	// every call splits one chat into one-shot sessions, so the prefix cache
	// never matches and every turn is billed uncached. Derive both from the
	// account and the gateway session, the same way WorkBuddy reuses
	// X-Conversation-ID. Without a session there is nothing stable to reuse,
	// so the ids stay per-request.
	conversationID := conversationIDFor(uid, session)
	obj["conversation_id"] = conversationID
	obj["session_id"] = conversationID
	if trimmed := strings.TrimSpace(uid); trimmed != "" {
		obj["user_id"] = trimmed
	}
}

// conversationIDFor derives the id Trae reuses as conversation_id and
// session_id for one account and one chat. An empty session has no stable
// id, so the caller falls back to a fresh one. Two accounts never share an
// id even when their session keys collide.
func conversationIDFor(uid, session string) string {
	session = strings.TrimSpace(session)
	if session == "" {
		return newConversationID()
	}
	sum := sha256.Sum256([]byte("trae-conversation|" + strings.TrimSpace(uid) + "|" + session))
	return hex.EncodeToString(sum[:16])
}

func newConversationID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return hex.EncodeToString(raw)
	}
	return hex.EncodeToString(raw)
}

// resolvedReasoningLevel returns the clamped reasoning level actually sent
// upstream for the given request, or empty when no level is included.
func resolvedReasoningLevel(req translate.ChatRequest, storedLevel string, caps providers.ModelCapabilities) string {
	if providers.RequestsDisabledThinking(req) {
		return "none"
	}
	level := requestedReasoningLevel(req)
	if level == "" {
		level = storedLevel
	}
	return clampReasoningLevel(level, caps)
}
