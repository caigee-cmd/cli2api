package qauth

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserAgentSplitsByEdition(t *testing.T) {
	if got := UserAgent(CN); got != "QoderWork/"+CosyVersion {
		t.Fatalf("CN UA=%q", got)
	}
	if got := UserAgent(Global); got != "Qoder/"+CosyVersion {
		t.Fatalf("global UA=%q", got)
	}
	if UserAgentForURL("https://api1.qoder.sh/algo/x") != UserAgent(Global) ||
		UserAgentForURL("https://gateway.qoder.com.cn/algo/x") != UserAgent(CN) {
		t.Fatal("URL edition inference wrong")
	}
	if openAPIUserAgentVersion != CosyVersion {
		t.Fatal("OpenAPI UA version must follow CosyVersion")
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
	if got.Get("Cosy-Version") != CosyVersion || strings.Contains(got.Get("User-Agent"), "Go-http-client") ||
		got.Get("User-Agent") != UserAgent(CN) {
		t.Fatalf("json headers: ver=%q ua=%q", got.Get("Cosy-Version"), got.Get("User-Agent"))
	}
	if got.Get("Cosy-MachineOS") != "" || got.Get("Cosy-MachineHostname") != "" {
		t.Fatal("machine os/hostname must be SSE-only")
	}

	_ = OpenStreamLines(context.Background(), sess, srv.URL+"/algo/api/chat", []byte(`{}`), nil, func(string) error { return nil })
	if got.Get("Cosy-MachineOS") != CosyMachineOS || got.Get("Cosy-MachineHostname") != MachineHostname("uid-test") {
		t.Fatalf("sse headers os=%q host=%q", got.Get("Cosy-MachineOS"), got.Get("Cosy-MachineHostname"))
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
