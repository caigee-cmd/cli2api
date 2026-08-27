package translate

import (
	"reflect"
	"testing"
)

func TestDropSystemMessagesRemovesSystemAndDeveloperOnly(t *testing.T) {
	req := ChatRequest{
		Model: "glm-5.2",
		Messages: []ChatMessage{
			{Role: "system", Content: "you are a bot"},
			{Role: "developer", Content: "dev note"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
	}
	out := DropSystemMessages(req)
	if len(out.Messages) != 2 {
		t.Fatalf("messages=%+v", out.Messages)
	}
	got := []string{out.Messages[0].Role, out.Messages[1].Role}
	if !reflect.DeepEqual(got, []string{"user", "assistant"}) {
		t.Fatalf("roles=%v", got)
	}
	if out.Model != req.Model {
		t.Fatalf("model changed: %s", out.Model)
	}
}

func TestDropSystemMessagesKeepsOriginalRequestUntouched(t *testing.T) {
	req := ChatRequest{
		Messages: []ChatMessage{
			{Role: "system", Content: "identity"},
			{Role: "user", Content: "hi"},
		},
	}
	out := DropSystemMessages(req)
	if len(out.Messages) != 1 {
		t.Fatalf("dropped copy=%+v", out.Messages)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("original request mutated: %+v", req.Messages)
	}
}
