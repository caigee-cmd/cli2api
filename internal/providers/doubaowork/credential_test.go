package doubaowork

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCookiesFromHeaderVariants(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		wantID string
	}{
		{"plain", "sessionid=abc123; ttwid=xyz", "abc123"},
		{"with prefix", "Cookie: sessionid_ss=ss999; s_v_web_id=verify_1", "ss999"},
	}
	for _, tc := range cases {
		cookies, err := CookiesFromHeader(tc.input)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !hasSessionCookie(cookies) {
			t.Fatalf("%s: missing session cookie", tc.name)
		}
	}
}

func TestDecodeCredentialThreeInputs(t *testing.T) {
	header := "sessionid=abc; s_v_web_id=verify_777; web_id=123456"

	fromHeader, err := DecodeCredential([]byte(header))
	if err != nil {
		t.Fatalf("raw header: %v", err)
	}
	if !fromHeader.Ready() {
		t.Fatal("raw header credential not ready")
	}
	if fromHeader.ClientInfo.Fingerprint != "verify_777" {
		t.Fatalf("fingerprint not taken from s_v_web_id: %q", fromHeader.ClientInfo.Fingerprint)
	}
	if fromHeader.ClientInfo.WebID != "123456" {
		t.Fatalf("web_id not parsed: %q", fromHeader.ClientInfo.WebID)
	}

	jsonList := `[{"name":"sessionid","value":"abc"},{"name":"ttwid","value":"t1"}]`
	fromList, err := DecodeCredential([]byte(jsonList))
	if err != nil || !fromList.Ready() {
		t.Fatalf("json list: %v (ready=%v)", err, fromList.Ready())
	}

	jsonObj := `{"format":"doubaowork-cookie-v1","cookies":[{"name":"sessionid_ss","value":"ss"}]}`
	fromObj, err := DecodeCredential([]byte(jsonObj))
	if err != nil || !fromObj.Ready() {
		t.Fatalf("json object: %v (ready=%v)", err, fromObj.Ready())
	}
}

func TestDecodeCredentialRejectsMissingSession(t *testing.T) {
	if _, err := DecodeCredential([]byte("foo=bar; baz=qux")); err == nil {
		t.Fatal("expected rejection when no sessionid present")
	}
	if err := ValidateCredential([]byte("{bad json")); err == nil {
		t.Fatal("expected rejection of corrupt json with no cookie fallback")
	}
}

func TestCredentialEncodeRoundTrip(t *testing.T) {
	raw := `{"cookies":[{"name":"sessionid","value":"abc"}],"uid":"u1","bot":"b1"}`
	cred, err := DecodeCredential([]byte(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	encoded, err := cred.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var back Credential
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if back.Format != CredentialFormat || back.UID != "u1" || !back.Ready() {
		t.Fatalf("unexpected round trip: %+v", back)
	}
	// Device identifiers must not be synthesized when absent.
	if back.ClientInfo.Fingerprint != "" {
		t.Fatalf("fingerprint must stay empty when absent, got %q", back.ClientInfo.Fingerprint)
	}
}

func TestPrepareImportValidatesAndNormalizes(t *testing.T) {
	codec := credentialCodec{}
	prepared, err := codec.PrepareImport([]byte("sessionid=abc"))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prepared.Ready {
		t.Fatal("prepared import should be ready")
	}
	if !strings.Contains(string(prepared.Payload), CredentialFormat) {
		t.Fatalf("payload missing canonical format: %s", prepared.Payload)
	}
	if _, err := codec.PrepareImport([]byte("nope=1")); err == nil {
		t.Fatal("expected import rejection without session cookie")
	}
}
