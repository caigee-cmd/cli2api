package devin

import (
	"bytes"
	"math"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// Inspect numeric wire fields independently of the generated schema.
func wireField(t *testing.T, data []byte, wanted protowire.Number) []byte {
	t.Helper()
	for len(data) > 0 {
		number, kind, n := protowire.ConsumeTag(data)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		data = data[n:]
		n = protowire.ConsumeFieldValue(number, kind, data)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		if number == wanted {
			return data[:n]
		}
		data = data[n:]
	}
	t.Fatalf("missing wire field %d", wanted)
	return nil
}

func wireMessage(t *testing.T, data []byte, number protowire.Number) []byte {
	t.Helper()
	value, n := protowire.ConsumeBytes(wireField(t, data, number))
	if n < 0 {
		t.Fatal(protowire.ParseError(n))
	}
	return value
}

func TestChatRequestWireContract(t *testing.T) {
	temperature := 0.25
	request, err := BuildGetChatMessageRequest("test-token", "test-device", "swe-2", "system",
		[]Prompt{{MessageID: "message", Source: 2, Content: "answer", ToolCallID: "result",
			ToolCalls: []ToolCall{{ID: "call", Name: "lookup", Arguments: `{}`}},
			Images:    []Image{{Base64Data: "aGVsbG8=", MimeType: "image/png"}},
			Thinking:  "reason", Signature: []byte("signature"), SignatureType: "type"}},
		[]Tool{{Name: "lookup", Description: "description", Parameters: []byte(`{"type":"object"}`)}},
		&temperature, 1234, t.Name(), "cascade")
	if err != nil {
		t.Fatal(err)
	}
	check := func(data []byte, fields map[protowire.Number][]byte) {
		t.Helper()
		for number, expected := range fields {
			if actual := wireField(t, data, number); !bytes.Equal(actual, expected) {
				t.Errorf("wire field %d = %x, want %x", number, actual, expected)
			}
		}
	}
	str := func(s string) []byte { return protowire.AppendString(nil, s) }
	check(request, map[protowire.Number][]byte{2: str("system"), 7: {5}, 16: str("cascade"), 20: {1}, 21: str("swe-2")})
	check(wireMessage(t, request, 1), map[protowire.Number][]byte{1: str(ClientProductLabel), 3: str("test-token"), 31: str(GenerateDeviceFingerprint("test-device"))})
	check(wireMessage(t, request, 8), map[protowire.Number][]byte{
		1: {1}, 2: protowire.AppendVarint(nil, 1234), 3: protowire.AppendVarint(nil, 400),
		5: protowire.AppendFixed64(nil, math.Float64bits(temperature)), 7: {40},
		8: protowire.AppendFixed64(nil, math.Float64bits(float64(float32(0.95)))),
	})
	prompt := wireMessage(t, request, 3)
	check(prompt, map[protowire.Number][]byte{1: str("message"), 2: {2}, 3: str("answer"), 7: str("result"), 11: str("reason"), 12: str("signature"), 18: str("type")})
	check(wireMessage(t, prompt, 6), map[protowire.Number][]byte{1: str("call"), 2: str("lookup"), 3: str(`{}`)})
	check(wireMessage(t, prompt, 10), map[protowire.Number][]byte{1: str("aGVsbG8="), 2: str("image/png")})
	check(wireMessage(t, request, 10), map[protowire.Number][]byte{1: str("lookup"), 2: str("description"), 3: str(`{"type":"object"}`)})
	check(wireMessage(t, request, 15), map[protowire.Number][]byte{1: str(t.Name()), 3: {4}})
}

func TestResponseWireUnknownAndMalformedFields(t *testing.T) {
	// Text plus a future field 99 must retain the known content.
	frame, err := ParseFrame([]byte{0x1a, 0x02, 'o', 'k', 0x98, 0x06, 0x01})
	if err != nil || frame.ContentText != "ok" {
		t.Fatalf("frame = %+v, error = %v", frame, err)
	}
	for _, payload := range [][]byte{{0x1a, 0x02, 'x'}, {0x3a, 0x01, 0x10}, {0x80}} {
		if _, err := ParseFrame(payload); err == nil {
			t.Errorf("accepted malformed response %x", payload)
		}
	}
	if _, err := ParseGetUserStatusResponse([]byte{0x0a, 0x01, 0x1a}); err == nil {
		t.Fatal("accepted malformed nested user status")
	}
}
