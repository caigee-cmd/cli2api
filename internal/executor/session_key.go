package executor

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

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
	bucket := int(sum[0]) % shards
	return namespace + "\x00" + hex.EncodeToString(sum[:8]) + "\x00" + string(rune('0'+bucket))
}

func shortSessionKey(key string) string {
	if len(key) <= 12 {
		return key
	}
	return key[:12]
}

// MessageStatShards mirrors workbuddy2api-hub's WB_MSGSTAT_SHARDS. Default 0
// keeps historical routing unchanged. A positive value only fills in when a
// request has no conversation id and no content seed, so it never pulls a
// chat that already has a stable fingerprint onto a coarser bucket.
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

// ShardFallbackKey returns a stable affinity key only when sharding is enabled.
// Callers use it when the request has no conversation id and no content seed.
func ShardFallbackKey(identity auth.Identity, req translate.ChatRequest) string {
	return ShardKeyFor(identity, req, MessageStatShards())
}
