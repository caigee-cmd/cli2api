package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResponsesResponseMapsLengthToIncomplete(t *testing.T) {
	response := responsesResponse("req", "model", "", "reasoning", nil, 10, 32, "length")
	if response["status"] != "incomplete" {
		t.Fatalf("status=%v", response["status"])
	}
	details := response["incomplete_details"].(map[string]any)
	if details["reason"] != "max_output_tokens" {
		t.Fatalf("details=%#v", details)
	}
	if responsesRequestStatus("length") != "incomplete" || responsesRequestStatus("stop") != "ok" {
		t.Fatal("request status mapping changed")
	}
}

func TestRelayResponsesStreamEmitsIncomplete(t *testing.T) {
	upstream := strings.NewReader("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"thinking\"}}]}\n\n" +
		"data: {\"choices\":[{\"finish_reason\":\"length\"}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":32}}\n\n" +
		"data: [DONE]\n\n")
	var output strings.Builder
	stats, err := RelayResponsesStream(&output, upstream, "req", "model", nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.FinishReason != "length" {
		t.Fatalf("finish reason=%q", stats.FinishReason)
	}
	body := output.String()
	if !strings.Contains(body, "event: response.incomplete") || strings.Contains(body, "event: response.completed") {
		t.Fatalf("wrong terminal event:\n%s", body)
	}
	var terminal map[string]any
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event) == nil && event["type"] == "response.incomplete" {
			terminal = event
		}
	}
	if terminal == nil {
		t.Fatal("missing response.incomplete payload")
	}
	response := terminal["response"].(map[string]any)
	if response["status"] != "incomplete" || response["incomplete_details"].(map[string]any)["reason"] != "max_output_tokens" {
		t.Fatalf("response=%#v", response)
	}
}
