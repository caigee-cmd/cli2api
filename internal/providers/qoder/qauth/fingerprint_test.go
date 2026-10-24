package qauth

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserAgentIsUndici(t *testing.T) {
	if got := UserAgent(CN); got != CosyUserAgent {
		t.Fatalf("CN UA=%q want %q", got, CosyUserAgent)
	}
	if got := UserAgent(Global); got != CosyUserAgent {
		t.Fatalf("global UA=%q want %q", got, CosyUserAgent)
	}
	if UserAgentForURL("https://api1.qoder.sh/algo/x") != CosyUserAgent ||
		UserAgentForURL("https://gateway.qoder.com.cn/algo/x") != CosyUserAgent {
		t.Fatal("URL UA must be undici for both editions")
	}
	if openAPIUserAgentVersion != CosyVersion {
		t.Fatal("OpenAPI UA version must follow CosyVersion")
	}
	if CosyVersion != "1.1.75" {
		t.Fatalf("CosyVersion=%q want 1.1.75", CosyVersion)
	}
	if DesktopCosyVersion != "0.4.8" {
		t.Fatalf("DesktopCosyVersion=%q want 0.4.8", DesktopCosyVersion)
	}
}

func TestMachineHostnameStablePerAccount(t *testing.T) {
	a, b, c := MachineHostname("uid-a"), MachineHostname("uid-a"), MachineHostname("uid-b")
	if a != b || a == c || !strings.HasPrefix(a, "DESKTOP-") || len(a) != 15 {
		t.Fatalf("a=%q b=%q c=%q", a, b, c)
	}
}

func TestBuildPayloadUsesCosyVersion(t *testing.T) {
	if !strings.Contains(payloadJSON(t, BuildPayloadB64("i")), `"cosyVersion":"`+CosyVersion+`"`) {
		t.Fatal("payload cosyVersion drifted")
	}
}

func TestBearerHeadersFingerprint(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		if r.Header.Get("Accept") == "text/event-stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"body\":\"[DONE]\"}\n\n"))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	sess := newTestSession(t)

	_, _ = CallGet(context.Background(), sess, srv.URL+"/algo/api/x")
	if got.Get("Cosy-Version") != CosyVersion || got.Get("User-Agent") != CosyUserAgent {
		t.Fatalf("json headers: ver=%q ua=%q", got.Get("Cosy-Version"), got.Get("User-Agent"))
	}
	if got.Get("Cosy-Data-Policy") != "agree" {
		t.Fatalf("data-policy=%q want agree", got.Get("Cosy-Data-Policy"))
	}
	if got.Get("Cosy-Business-Product") != "cli" || got.Get("Cosy-Business-Type") != "agent" ||
		got.Get("Cosy-Scene") != "assistant" || got.Get("Cosy-Clientip") != CosyClientIP {
		t.Fatalf("business headers missing: product=%q type=%q scene=%q ip=%q",
			got.Get("Cosy-Business-Product"), got.Get("Cosy-Business-Type"),
			got.Get("Cosy-Scene"), got.Get("Cosy-Clientip"))
	}
	// qodercli 1.1.75: machineos on all COSY requests; hostname SSE-only.
	if got.Get("Cosy-MachineOS") != CosyMachineOS {
		t.Fatalf("json Cosy-MachineOS=%q want %q", got.Get("Cosy-MachineOS"), CosyMachineOS)
	}
	if got.Get("Cosy-MachineHostname") != "" {
		t.Fatal("machine hostname must be SSE-only")
	}

	_ = OpenStreamLines(context.Background(), sess, srv.URL+"/algo/api/chat", []byte(`{}`), nil, func(string) error { return nil })
	if got.Get("Cosy-MachineOS") != CosyMachineOS || got.Get("Cosy-MachineHostname") != MachineHostname("uid-test") {
		t.Fatalf("sse headers os=%q host=%q", got.Get("Cosy-MachineOS"), got.Get("Cosy-MachineHostname"))
	}
	if got.Get("User-Agent") != CosyUserAgent {
		t.Fatalf("sse UA=%q", got.Get("User-Agent"))
	}
}

func payloadJSON(t *testing.T, b64 string) string {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
