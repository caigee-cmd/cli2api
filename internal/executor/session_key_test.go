package executor

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestSessionKeyForConversationAliases(t *testing.T) {
	req := translate.ChatRequest{Model: "hy4free", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	identity := auth.ConsoleIdentity()
	base := SessionKeyFor("", identity, req)
	explicit := sessionKeyFor("", "chat-42", identity, req)
	if explicit == "" || explicit == base {
		t.Fatalf("conversation id must override content seed: base=%q explicit=%q", base, explicit)
	}
	if got := sessionKeyFor("header-1", "chat-42", identity, req); got == explicit {
		t.Fatal("header must win over conversation id")
	}
	if got := sessionKeyFor("header-1", "chat-42", identity, req); got != sessionKeyFor("header-1", "ignored", identity, req) {
		t.Fatal("conversation id must not change a header-pinned key")
	}
}

func TestShardKeyStableAcrossTurns(t *testing.T) {
	identity := auth.ConsoleIdentity()
	first := translate.ChatRequest{Model: "hy4free", Messages: []translate.ChatMessage{
		{Role: "system", Content: "stable system"},
		{Role: "user", Content: "first question"},
	}}
	later := translate.ChatRequest{Model: "hy4free", Messages: []translate.ChatMessage{
		{Role: "system", Content: "stable system"},
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "follow up"},
	}}
	a := ShardKeyFor(identity, first, 4)
	b := ShardKeyFor(identity, later, 4)
	if a == "" || a != b {
		t.Fatalf("shard broke across turns: %q vs %q", a, b)
	}
	if ShardKeyFor(identity, first, 0) != "" {
		t.Fatal("disabled sharding must return empty")
	}
	other := translate.ChatRequest{Model: "hy3", Messages: first.Messages}
	if ShardKeyFor(identity, other, 4) == a {
		t.Fatal("different model must not share a shard")
	}
}

func TestPrepareUsesConversationID(t *testing.T) {
	ex := NewChatExecutor(NewPool(nil, nil), "")
	req := translate.ChatRequest{Model: "glm-5.2", Messages: []translate.ChatMessage{{Role: "user", Content: "hi"}}}
	plain, err := ex.Prepare(PrepareInput{Request: req, Identity: auth.ConsoleIdentity(), CrossProviderPool: true})
	if err != nil {
		t.Fatal(err)
	}
	withID, err := ex.Prepare(PrepareInput{Request: req, Identity: auth.ConsoleIdentity(), ConversationID: "conv-9", CrossProviderPool: true})
	if err != nil {
		t.Fatal(err)
	}
	if sessionKeyFromContext(plain.Context) == sessionKeyFromContext(withID.Context) {
		t.Fatal("conversation id must change the affinity key")
	}
	if providers.ConversationFromContext(withID.Context) == "" {
		t.Fatal("conversation id must reach provider headers")
	}
}


func TestMessageStatShardsEnvAndFallback(t *testing.T) {
	if MessageStatShards() != 0 {
		t.Fatal("sharding must be off by default")
	}
	t.Setenv("CLI2API_MSGSTAT_SHARDS", "4")
	if MessageStatShards() != 4 {
		t.Fatal("env not applied")
	}
	req := translate.ChatRequest{Model: "hy4free", Messages: []translate.ChatMessage{
		{Role: "system", Content: "stable"},
		{Role: "user", Content: "one"},
	}}
	later := translate.ChatRequest{Model: "hy4free", Messages: []translate.ChatMessage{
		{Role: "system", Content: "stable"},
		{Role: "user", Content: "one"},
		{Role: "assistant", Content: "answer"},
		{Role: "user", Content: "two"},
	}}
	a := ShardFallbackKey(auth.ConsoleIdentity(), req)
	b := ShardFallbackKey(auth.ConsoleIdentity(), later)
	if a == "" || a != b {
		t.Fatalf("shard fallback broke across turns: %q %q", a, b)
	}
	ex := NewChatExecutor(NewPool(nil, nil), "")
	prepared, err := ex.Prepare(PrepareInput{Request: req, Identity: auth.ConsoleIdentity(), CrossProviderPool: true})
	if err != nil {
		t.Fatal(err)
	}
	if sessionKeyFromContext(prepared.Context) != a {
		t.Fatal("Prepare did not use shard fallback")
	}
	t.Setenv("CLI2API_MSGSTAT_SHARDS", "bad")
	if MessageStatShards() != 0 {
		t.Fatal("invalid env must disable sharding")
	}
}
