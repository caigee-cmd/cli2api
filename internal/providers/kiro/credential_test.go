package kiro

import (
	"strings"
	"testing"
)

func TestDecodeUpstreamBuilderIDKeepsRegionAndName(t *testing.T) {
	raw := []byte(`{
		"authMethod":"IdC","provider":"BuilderId","region":"eu-central-1",
		"accessToken":"access","refreshToken":"refresh","expiresAt":"2026-12-01T00:00:00Z",
		"clientId":"client","clientSecret":"secret",
		"profileArn":"arn:aws:codewhisperer:us-east-1:123456789012:profile/abc",
		"email":"dev@example.com","machineId":"machine-1"
	}`)
	cred, err := DecodeCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AuthMethod != "idc" || cred.Region != "eu-central-1" || cred.Email != "dev@example.com" {
		t.Fatalf("cred = %+v", cred)
	}
	if cred.importRegion(credentialStatesRegion(raw)) != "us-east-1" {
		t.Fatal("profile arn region did not win")
	}
	if cred.importName() != "dev@example.com" {
		t.Fatalf("name = %s", cred.importName())
	}
	if cred.ExpiresAt == 0 {
		t.Fatal("rfc3339 expiry was dropped")
	}
}

func TestDecodeAPIKeySplitsRegionAndDerivesMachineID(t *testing.T) {
	raw := []byte(`{"kiroApiKey":"ksk_test|eu-central-1"}`)
	cred, err := DecodeCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AuthMethod != "api_key" || cred.AccessToken != "ksk_test" || cred.Region != "eu-central-1" {
		t.Fatalf("cred = %+v", cred)
	}
	if cred.RefreshToken != "" || cred.ProfileArn != "" || cred.ExpiresAt != 0 {
		t.Fatalf("api key kept oauth fields: %+v", cred)
	}
	if cred.MachineID != machineIDFromAPIKey("ksk_test") || !strings.HasPrefix(cred.Email, "api-key-") {
		t.Fatalf("fingerprint = %s %s", cred.MachineID, cred.Email)
	}
	if cred.importRegion(credentialStatesRegion(raw)) != "eu-central-1" {
		t.Fatal("suffix region was not stated")
	}
	encoded, err := cred.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "ksk_test|") {
		t.Fatalf("region suffix stored: %s", encoded)
	}
}

func TestSplitImportUnwrapsKiroAccountPack(t *testing.T) {
	raw := []byte(`{"accounts":[
		{"authMethod":"builder_id","region":"us-east-1","refreshToken":"a","clientId":"c","clientSecret":"s","email":"us@example.com"},
		{"kiroApiKey":"ksk_test|eu-central-1"}
	]}`)
	chunks, err := splitCredentialImport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	first, err := prepareCredentialImport(chunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Region != "us-east-1" || first.Name != "us@example.com" {
		t.Fatalf("first = %+v", first)
	}
	second, err := prepareCredentialImport(chunks[1])
	if err != nil {
		t.Fatal(err)
	}
	if second.Region != "eu-central-1" {
		t.Fatalf("second region = %s", second.Region)
	}
}

func TestSplitImportLeavesWorkBuddyPackAlone(t *testing.T) {
	raw := []byte(`{"accounts":[{"accessToken":"a","domain":"www.workbuddy.cn","uid":"u"}]}`)
	chunks, err := splitCredentialImport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || string(chunks[0]) != string(raw) {
		t.Fatalf("workbuddy pack was unwrapped: %d", len(chunks))
	}
}

func TestDecodeGitHubSocialArray(t *testing.T) {
	raw := []byte(`[{"email":"dev@icloud.com","accessToken":"access","refreshToken":"refresh","provider":"Github"}]`)
	chunks, err := splitCredentialImport(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks = %d", len(chunks))
	}
	prepared, err := prepareCredentialImport(chunks[0])
	if err != nil {
		t.Fatal(err)
	}
	cred, err := DecodeCredential(prepared.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AuthMethod != "social" || cred.Provider != "Github" || cred.RefreshToken != "refresh" || cred.Email != "dev@icloud.com" {
		t.Fatalf("cred = %+v", cred)
	}
	if prepared.Name != "dev@icloud.com" {
		t.Fatalf("name = %s", prepared.Name)
	}
}

func TestDecodeOmitsRegionWhenJSONDoesNotStateOne(t *testing.T) {
	raw := []byte(`{"accessToken":"access","refreshToken":"refresh","clientId":"c","clientSecret":"s"}`)
	cred, err := DecodeCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cred.importRegion(credentialStatesRegion(raw)) != "" {
		t.Fatal("an unstated region overrode the operator choice")
	}
	if cred.AuthMethod != "builder_id" || cred.Region != DefaultRegion {
		t.Fatalf("cred = %+v", cred)
	}
}
