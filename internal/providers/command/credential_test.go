package command

import "testing"

func TestValidateCredential(t *testing.T) {
	ok := []string{
		`{"api_key":"user_abcdefghijklmnop"}`,
		`{"apiKey":"user_abcdefghijklmnop"}`,
		`{"key":"user_abcdefghijklmnop"}`,
		`{"token":" user_abcdefghijklmnop "}`,
	}
	for _, payload := range ok {
		if err := ValidateCredential([]byte(payload)); err != nil {
			t.Errorf("ValidateCredential(%s) = %v, want nil", payload, err)
		}
	}

	bad := []string{
		`{}`,
		`{"api_key":""}`,
		`{"api_key":"not-a-user-key"}`,
		`not json`,
	}
	for _, payload := range bad {
		if err := ValidateCredential([]byte(payload)); err == nil {
			t.Errorf("ValidateCredential(%s) = nil, want error", payload)
		}
	}
}

func TestFormatAPIKeyStripsBearer(t *testing.T) {
	cases := map[string]string{
		"  user_abc  ":    "user_abc",
		"Bearer user_abc": "user_abc",
		"bearer user_abc": "user_abc",
		"user_abc":        "user_abc",
	}
	for in, want := range cases {
		if got := FormatAPIKey(in); got != want {
			t.Errorf("FormatAPIKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeCredentialDefaults(t *testing.T) {
	cred, err := DecodeCredential([]byte(`{"apiKey":"user_abc"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cred.Format != CredentialFormat {
		t.Errorf("format = %q", cred.Format)
	}
	if cred.APIKey != "user_abc" {
		t.Errorf("api key = %q", cred.APIKey)
	}
	encoded, err := cred.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	round, err := DecodeCredential(encoded)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if round.BaseURL != BaseURL {
		t.Errorf("base url default = %q, want %q", round.BaseURL, BaseURL)
	}
}

func TestPrepareImport(t *testing.T) {
	prepared, err := (credentialCodec{}).PrepareImport([]byte(`{"api_key":"user_abcdefghijklmnop"}`))
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if !prepared.Ready {
		t.Error("a valid key must be ready without an identity probe")
	}
	if _, err := (credentialCodec{}).PrepareImport([]byte(`{"api_key":"bad"}`)); err == nil {
		t.Error("a non-user_ key must be rejected")
	}
}
