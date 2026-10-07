package qoder

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type memoryCredentials struct {
	credential accounts.NativeCredential
	region     string
}

func (m memoryCredentials) LoadCredential(context.Context, string) (accounts.NativeCredential, error) {
	return m.credential, nil
}

func (m memoryCredentials) SaveCredential(context.Context, string, string, accounts.NativeCredential) error {
	return nil
}

func TestDirectChatPostsEncodedBodyAndReadsNestedSSE(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		gotAuth = r.Header.Get("Authorization")
		gotModel = r.Header.Get("X-Model-Key")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"body\":{\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"credits\":0.2}}}\n\n")
	}))
	defer upstream.Close()

	direct := NewDirect(memoryCredentials{
		credential: accounts.NativeCredential{
			UserBlob:  []byte(`{"uid":"u-1","name":"Ada","access_token":"dt-token"}`),
			MachineID: "0123456789abcdef",
		},
		region: "cn",
	}, func(context.Context, string) (string, error) { return "cn", nil })
	direct.now = func() time.Time { return time.Unix(1_700_000_000, 0) }
	direct.SetHTTP(upstream.Client())

	original := ChatEndpointHook
	ChatEndpointHook = func(string) string {
		return upstream.URL + "/algo/api/v2/service/pro/sse/agent_chat_generation?Encode=1"
	}
	t.Cleanup(func() { ChatEndpointHook = original })

	outcome, err := direct.ChatNonStream(context.Background(), "acc", translate.ChatRequest{
		Model:    "auto",
		Messages: []translate.ChatMessage{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Content != "OK" || outcome.PromptTokens != 3 || outcome.Credits == nil || *outcome.Credits != 0.2 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if !strings.HasPrefix(gotAuth, "Bearer COSY.") {
		t.Fatalf("auth = %q", gotAuth)
	}
	if gotModel != "auto" || !strings.Contains(gotPath, "Encode=1") {
		t.Fatalf("path=%s model=%s", gotPath, gotModel)
	}
	plain, err := DecodeBody(gotBody)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plain), `"content":"ping"`) || !strings.Contains(string(plain), `"agent_id":"agent_common"`) {
		t.Fatalf("plain = %s", plain)
	}
}

func TestDirectChatClassifiesUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer upstream.Close()
	direct := NewDirect(memoryCredentials{
		credential: accounts.NativeCredential{UserBlob: []byte(`{"uid":"u","access_token":"dt"}`), MachineID: "0123456789abcdef"},
	}, nil)
	direct.SetHTTP(upstream.Client())
	original := ChatEndpointHook
	ChatEndpointHook = func(string) string { return upstream.URL }
	t.Cleanup(func() { ChatEndpointHook = original })

	_, err := direct.ChatNonStream(context.Background(), "acc", translate.ChatRequest{Model: "auto"})
	classified, ok := err.(*providers.Error)
	if !ok || classified.Kind != accounts.KindAuth || classified.Status != http.StatusForbidden {
		t.Fatalf("err = %#v", err)
	}
}
