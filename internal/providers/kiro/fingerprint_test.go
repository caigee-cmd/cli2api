package kiro

import (
	"net/http"
	"strings"
	"testing"
)

func TestSDKUserAgentMatchesKiroIDE(t *testing.T) {
	cred := Credential{MachineID: "machine-1"}
	ua := sdkUserAgent(cred, "codewhispererstreaming", StreamingSDKVersion, "m/E")
	for _, want := range []string{
		"aws-sdk-js/" + StreamingSDKVersion,
		"os/" + systemFingerprint(),
		"md/nodejs#" + nodeVersion,
		"api/codewhispererstreaming#" + StreamingSDKVersion,
		"m/E",
		"KiroIDE-" + currentIDEVersion() + "-machine-1",
	} {
		if !strings.Contains(ua, want) {
			t.Fatalf("user agent %q missing %q", ua, want)
		}
	}
	if strings.Contains(ua, "os/darwin ") || strings.Contains(ua, "nodejs#22 ") {
		t.Fatalf("user agent still uses the short fingerprint: %s", ua)
	}
	if !strings.Contains(systemFingerprint(), "#") {
		t.Fatalf("system fingerprint %q has no release", systemFingerprint())
	}
}

func TestNewerIDEVersionRejectsStaleFeed(t *testing.T) {
	if newerIDEVersion("0.12.0", IDEVersion) {
		t.Fatal("a build older than the fallback was accepted")
	}
	if newerIDEVersion(IDEVersion, IDEVersion) {
		t.Fatal("the fallback was treated as newer than itself")
	}
	if !newerIDEVersion("1.5.0", IDEVersion) {
		t.Fatal("a newer build was rejected")
	}
	if newerIDEVersion("not-a-version", IDEVersion) {
		t.Fatal("an unparsed feed was treated as newer")
	}
}

func TestLegacyHeadersCarryInvocationID(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://q.us-east-1.amazonaws.com/generateAssistantResponse", nil)
	if err != nil {
		t.Fatal(err)
	}
	applyLegacyHeaders(req, Credential{AccessToken: "token"}, legacyEndpoint{}, 1)
	if req.Header.Get("Amz-Sdk-Invocation-Id") == "" {
		t.Fatal("legacy request omitted Amz-Sdk-Invocation-Id")
	}
	if req.Header.Get("x-amzn-codewhisperer-optout") != "true" {
		t.Fatalf("optout = %q", req.Header.Get("x-amzn-codewhisperer-optout"))
	}
	if !strings.Contains(req.Header.Get("User-Agent"), "os/"+systemFingerprint()) {
		t.Fatalf("user agent = %s", req.Header.Get("User-Agent"))
	}
}
