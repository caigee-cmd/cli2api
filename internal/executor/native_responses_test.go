package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type nativeOnlyStreamer struct {
	called bool
	model  string
	input  string
}

func (s *nativeOnlyStreamer) ResponsesStream(_ context.Context, _ string, req *translate.NativeResponsesRequest, options providers.RequestOptions) (*http.Response, providers.ResolvedChat, error) {
	s.called = true
	s.model = options.Model
	s.input = string(req.Fields()["input"])
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")),
		Header:     make(http.Header),
	}, providers.ResolvedChat{}, nil
}

func TestNativeResponsesDispatchDoesNotRequireChatCompatibility(t *testing.T) {
	native, err := translate.ParseNativeResponses([]byte(`{"model":"m","input":[{"type":"input_file","file_id":"file_1"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := native.Compat(); err == nil {
		t.Fatal("fixture must be incompatible with ChatRequest")
	}
	streamer := &nativeOnlyStreamer{}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "native", NativeResponses: streamer})
	pool := NewPool(nil, nil)
	pool.Upsert(Item{ID: "native-1", Provider: "native", Region: "global", Runtime: "in_process", Models: []string{"m"}})
	ex := NewChatExecutor(pool, "")
	ex.Providers = registry

	result, err := ex.ChatStreamProxyNativeResponses(context.Background(), translate.ChatRequest{Model: "m"}, native, "", "native")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Response.Body.Close()
	if !result.NativeResponses || !streamer.called {
		t.Fatalf("native dispatch result=%+v called=%v", result, streamer.called)
	}
	if streamer.model != "m" || !strings.Contains(streamer.input, `"input_file"`) {
		t.Fatalf("native request changed: model=%q input=%s", streamer.model, streamer.input)
	}
}
