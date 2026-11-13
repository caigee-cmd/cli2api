package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// sessionIDAliases mirrors the reference gateways' conversation-key lookup
// (Sliverkiss/workbuddy2api #35, qoder2api-hub session affinity). Clients use
// several spellings for the same conversation id; recognising only one makes
// turns rotate accounts and silently destroys the upstream prompt cache.
var sessionIDAliases = []string{
	"conversation_id", "conversationId", "conversation-id",
	"session_id", "sessionId", "session-id",
	"thread_id", "threadId",
	"conversation", "session",
}

// sessionKeySources carries every id a caller can use to pin one chat to one
// account. HTTP headers win over body metadata; the gateway's own header stays
// first for compatibility.
type sessionKeySources struct {
	Headers   []string
	BodyJSON  []byte
	SessionID string
}

// extractConversationKey returns the first non-empty, bounded conversation id
// found in headers, then in the request body (top level and metadata), then in
// the native Responses session seed. Unknown keys are ignored; no id is ever
// invented. Values are capped so a hostile client cannot pin unbounded memory.
func extractConversationKey(src sessionKeySources) string {
	for _, value := range src.Headers {
		if id := cleanConversationID(value); id != "" {
			return id
		}
	}
	if len(src.BodyJSON) > 0 && len(src.BodyJSON) <= 64<<20 {
		var body map[string]json.RawMessage
		if json.Unmarshal(src.BodyJSON, &body) == nil {
			if id := conversationIDFromBody(body); id != "" {
				return id
			}
		}
	}
	if id := cleanConversationID(src.SessionID); id != "" {
		return id
	}
	return ""
}

func conversationIDFromBody(body map[string]json.RawMessage) string {
	for _, key := range sessionIDAliases {
		if id, ok := rawConversationString(body[key]); ok {
			return id
		}
	}
	for _, metaKey := range []string{"metadata", "meta", "context"} {
		var meta map[string]json.RawMessage
		if json.Unmarshal(body[metaKey], &meta) != nil {
			continue
		}
		for _, key := range sessionIDAliases {
			if id, ok := rawConversationString(meta[key]); ok {
				return id
			}
		}
	}
	return ""
}

func rawConversationString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		if id := cleanConversationID(value); id != "" {
			return id, true
		}
	}
	return "", false
}

func cleanConversationID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 512 {
		return ""
	}
	return value
}

// sessionKeyFor is the shared implementation; PrepareInput carries an explicit
// conversation id in addition to the legacy header.
func sessionKeyFor(header, conversation string, identity auth.Identity, req translate.ChatRequest) string {
	raw := strings.TrimSpace(header)
	kind := "content"
	if raw == "" {
		raw = strings.TrimSpace(conversation)
	}
	if raw != "" {
		kind = "header"
	} else {
		raw = translate.ContentSessionSeed(req)
	}
	if raw == "" {
		return ""
	}
	namespace := "console"
	if identity.Kind == auth.KindKey && identity.KeyID != "" {
		namespace = "key:" + identity.KeyID
	}
	sum := sha256.Sum256([]byte(namespace + "\x00" + kind + "\x00" + raw))
	return hex.EncodeToString(sum[:])
}

// ShardKeyFor implements workbuddy2api-hub's WB_MSGSTAT_SHARDS idea with a pure
// function: one (key, model, tools, system-prefix) tuple maps to a small, stable
// set of accounts instead of being scattered by the first user text. The caller
// owns the account selection; shards is the maximum number of buckets.
func ShardKeyFor(identity auth.Identity, req translate.ChatRequest, shards int) string {
	if shards <= 0 {
		return ""
	}
	namespace := "console"
	if identity.Kind == auth.KindKey && identity.KeyID != "" {
		namespace = "key:" + identity.KeyID
	}
	seed := translate.PrefixSessionSeed(req)
	if seed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(namespace + "\x00shard\x00" + seed))
	bucket := int(sum[0])%shards
	return namespace + "\x00" + hex.EncodeToString(sum[:8]) + "\x00" + string(rune('0'+bucket))
}


func shortSessionKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12]
}


// MessageStatShards mirrors workbuddy2api-hub's WB_MSGSTAT_SHARDS. Default 0
// keeps historical routing unchanged; a positive value bounds one-shot
// /v1/messages calls to N account buckets per (key, model, system, tools).
func MessageStatShards() int {
	raw := strings.TrimSpace(os.Getenv("CLI2API_MSGSTAT_SHARDS"))
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 64 {
		return 0
	}
	return n
}

// ShardFallbackKey returns a stable, non-empty affinity key only when sharding
// is enabled. It never overrides an explicit conversation id.
func ShardFallbackKey(identity auth.Identity, req translate.ChatRequest) string {
	return ShardKeyFor(identity, req, MessageStatShards())
}
