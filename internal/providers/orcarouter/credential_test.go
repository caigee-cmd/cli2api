package orcarouter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Both entries must land on the same credential shape, and the downstream
// provider must not care which entry minted it.

func TestAPIKeyCredentialSaveLoadClearAndMask(t *testing.T) {
	store := newFakeStore()
	client := NewClient(store)

	// Save through the same seam the console uses (PrepareImport → store).
	importer := credentialCodec{}
	prepared, err := importer.PrepareImport([]byte(`{"api_key":"sk-orca-AAAABBBBCCCCDDDDEEEE"}`))
	if err != nil {
		t.Fatalf("prepare import: %v", err)
	}
	if !prepared.Ready {
		t.Fatal("a well-formed sk-orca key must import as ready")
	}
	if err := store.SaveCredentialPayload(context.Background(), "acc-1", CredentialFormat, prepared.Payload); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Read back.
	_, payload := store.stored()
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if credential.APIKey != "sk-orca-AAAABBBBCCCCDDDDEEEE" {
		t.Fatalf("unexpected key round-trip: %q", credential.APIKey)
	}
	if credential.Format != CredentialFormat {
		t.Fatalf("expected format %q, got %q", CredentialFormat, credential.Format)
	}
	if !credential.Ready() {
		t.Fatal("stored credential must be ready")
	}

	// The adapter must be able to redact it.
	masked := Redact("request failed for " + credential.APIKey)
	if strings.Contains(masked, "AAAABBBBCCCCDDDDEEEE") {
		t.Fatalf("redactor leaked the key: %s", masked)
	}
	if !strings.Contains(masked, "sk-orca-[redacted]") {
		t.Fatalf("redactor did not mask the key: %s", masked)
	}

	// Clear.
	if err := store.SaveCredentialPayload(context.Background(), "acc-1", CredentialFormat, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, cleared := store.stored(); len(cleared) != 0 {
		t.Fatalf("clearing must remove the payload, got %q", cleared)
	}
	if _, err := client.credential(context.Background(), "acc-1"); err == nil {
		t.Fatal("a cleared credential must not resolve")
	}
}

func TestValidateCredentialRejectsBadShapes(t *testing.T) {
	cases := map[string]string{
		"empty":       `{}`,
		"wrong sort":  `{"api_key":"sk-live-1234"}`,
		"not a key":   `{"api_key":"hello world"}`,
		"empty hdr":   `{"api_key":"sk-orca-1234567890","headers":{"":"x"}}`,
		"bad json":    `{`,
		"only format": `{"format":"orcarouter-key-v1"}`,
	}
	for name, payload := range cases {
		if err := ValidateCredential([]byte(payload)); err == nil {
			t.Fatalf("%s: expected validation failure", name)
		}
	}
	// The exchange response body is accepted verbatim on import.
	exchangeBody := `{"key":"sk-orca-1234567890","user_id":"12345","scope":"api"}`
	if err := ValidateCredential([]byte(exchangeBody)); err != nil {
		t.Fatalf("exchange response must import: %v", err)
	}
	credential, err := DecodeCredential([]byte(exchangeBody))
	if err != nil {
		t.Fatalf("decode exchange body: %v", err)
	}
	if credential.UserID != "12345" || credential.Scope != "api" {
		t.Fatalf("exchange identity not preserved: %+v", credential)
	}
}

// The API-key adapter and the PKCE adapter must produce the same credential
// result, and nothing downstream may branch on which entry minted it.
func TestBothAuthEntriesShareOneCredentialResult(t *testing.T) {
	apiKeyPayload, err := Credential{Format: CredentialFormat, APIKey: "sk-orca-SHAREDSHAREDSHARED"}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	oauthPayload, err := Credential{Format: CredentialFormatOAuth, APIKey: "sk-orca-SHAREDSHAREDSHARED"}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	apiCredential, err := DecodeCredential(apiKeyPayload)
	if err != nil {
		t.Fatalf("decode api key: %v", err)
	}
	oauthCredential, err := DecodeCredential(oauthPayload)
	if err != nil {
		t.Fatalf("decode oauth: %v", err)
	}
	if apiCredential.APIKey != oauthCredential.APIKey {
		t.Fatal("both entries must yield the same ordinary OrcaRouter API key")
	}
	if apiCredential.Ready() != oauthCredential.Ready() {
		t.Fatal("readiness must not depend on the entry")
	}
	// The credential is entry-agnostic for requests: the same bearer header, the
	// same inference base, no entry-specific branch.
	if apiCredential.APIKey == "" || oauthCredential.APIKey == "" {
		t.Fatal("both credentials must carry a usable key")
	}
}

func TestBothAdaptersShareChatCatalogAndAuth(t *testing.T) {
	store := newFakeStore()
	client := NewClient(store)
	apiAdapter := client.Adapter()
	oauthAdapter := client.OAuthAdapter()
	if apiAdapter.ID != ProviderID || oauthAdapter.ID != OAuthProviderID {
		t.Fatalf("unexpected adapter ids: %q %q", apiAdapter.ID, oauthAdapter.ID)
	}
	// One credential seam, one chat path, one catalog, one classifier.
	if apiAdapter.Credential == nil || oauthAdapter.Credential == nil {
		t.Fatal("both entries need a credential codec")
	}
	if apiAdapter.Models == nil || oauthAdapter.Models == nil {
		t.Fatal("both entries must expose the model catalog")
	}
	if apiAdapter.Chat == nil || oauthAdapter.Chat == nil {
		t.Fatal("both entries must route inference")
	}
	if apiAdapter.Login != nil {
		t.Fatal("the pasted-key entry must not start a PKCE login")
	}
	if oauthAdapter.Login == nil {
		t.Fatal("the auth entry must expose the PKCE login")
	}
}

func TestCredentialStateNeedsReauthBlocksUse(t *testing.T) {
	store := newFakeStore()
	payload, err := Credential{Format: CredentialFormatOAuth, APIKey: "sk-orca-REVOKEDKEY1234567890", State: CredentialStateNeedsReauth}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	store.format, store.payload = CredentialFormatOAuth, payload
	client := NewClient(store)
	if _, err := client.credential(context.Background(), "acc-1"); err == nil {
		t.Fatal("a revoked credential must not resolve")
	} else if !strings.Contains(err.Error(), "reconnect") && !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revocation must be reported as reauthentication, got %q", err.Error())
	}
}

// The 401 transition must change only the exact generation that made the
// rejected request, and must never attempt a refresh.
func TestMarkNeedsReauthIsGenerationSafe(t *testing.T) {
	store := newFakeStore()
	stale, _ := Credential{Format: CredentialFormatOAuth, APIKey: "sk-orca-OLDGEN0000000000000", Generation: 1}.Encode()
	store.format, store.payload = CredentialFormatOAuth, stale
	client := NewClient(store)

	// A newer login replaces the credential.
	fresh, _ := Credential{Format: CredentialFormatOAuth, APIKey: "sk-orca-NEWGEN0000000000000", Generation: 2}.Encode()
	store.format, store.payload = CredentialFormatOAuth, fresh

	// The late 401 from generation 1 arrives now.
	client.markNeedsReauth(context.Background(), "acc-1", 1, 401)

	_, payload := store.stored()
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if credential.NeedsReauth() {
		t.Fatal("a stale 401 must not mark a newly authorized credential")
	}
	if credential.APIKey != "sk-orca-NEWGEN0000000000000" {
		t.Fatalf("stale failure overwrote the new key: %q", credential.APIKey)
	}
	if credential.Generation != 2 {
		t.Fatalf("generation changed: %d", credential.Generation)
	}

	// Now the current generation is rejected.
	client.markNeedsReauth(context.Background(), "acc-1", 2, 401)
	_, payload = store.stored()
	credential, err = DecodeCredential(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !credential.NeedsReauth() {
		t.Fatal("the rejected generation must be marked needs_reauth")
	}
	if credential.APIKey != "sk-orca-NEWGEN0000000000000" {
		t.Fatal("marking must not delete or replace the stored secret")
	}
	if store.kinds["acc-1"] != "auth" {
		t.Fatalf("expected auth error kind, got %q", store.kinds["acc-1"])
	}
	if store.observed["acc-1"] != "login_required" {
		t.Fatalf("expected login_required status, got %q", store.observed["acc-1"])
	}
}

// A revoked durable key must not trigger any refresh request: there is no
// refresh grant. This asserts the adapter's whole outbound call set.
func TestRevokedKeyDoesNotAttemptRefresh(t *testing.T) {
	hits := &hitCounter{paths: map[string]int{}}
	srv := newJSONServer(t, hits, func(path string) (int, string) {
		return 401, `{"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}`
	})
	store := newFakeStore()
	payload, _ := Credential{Format: CredentialFormat, APIKey: "sk-orca-REVOKED00000000000", Generation: 1, APIBase: srv.URL}.Encode()
	store.format, store.payload = CredentialFormat, payload
	client := NewClient(store)

	_, err := client.ChatNonStream(context.Background(), "acc-1", chatRequest("deepseek/deepseek-v4-pro"))
	if err == nil {
		t.Fatal("a 401 must surface as an error")
	}
	for path := range hits.snapshot() {
		if strings.Contains(path, "refresh") || strings.Contains(path, "token") {
			t.Fatalf("a revoked key must not attempt a refresh grant, saw %q", path)
		}
		if strings.Contains(path, "/auth/") {
			t.Fatalf("inference must never call the auth origin, saw %q", path)
		}
	}
	_, stored := store.stored()
	credential, _ := DecodeCredential(stored)
	if !credential.NeedsReauth() {
		t.Fatal("401 must mark the exact generation needs_reauth")
	}
}

func TestJSONErrorEnvelopeClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"revoked", 401, `{"error":{"message":"Invalid API key","code":"invalid_api_key"}}`, "auth"},
		{"scope refused", 403, `{"error":{"message":"forbidden"}}`, "auth"},
		{"rate limit", 429, `{"error":{"message":"rate limit exceeded"}}`, "rate_limit"},
		{"no balance", 402, `{"error":{"message":"insufficient quota"}}`, "quota"},
		{"bad model", 404, `{"error":{"message":"model not found"}}`, "model_not_available"},
		{"bad body", 400, `{"error":{"message":"invalid_request_error"}}`, "invalid_request"},
		{"upstream", 502, `{"error":{"message":"no available channel"}}`, "unavailable"},
	}
	for _, tc := range cases {
		got := Classify(tc.status, tc.body)
		if got.Kind != tc.want {
			t.Fatalf("%s: expected kind %q, got %q (%s)", tc.name, tc.want, got.Kind, got.Message)
		}
	}
}

func TestRedactorMasksVerifierAndState(t *testing.T) {
	text := `POST /api/v1/auth/keys?state=abc123def456 {"code_verifier":"VERIFIER123456","code_challenge":"CHALLENGE12345"} Authorization: Bearer sk-orca-SECRETKEY123456`
	masked := Redact(text)
	for _, secret := range []string{"VERIFIER123456", "CHALLENGE12345", "SECRETKEY123456", "abc123def456"} {
		if strings.Contains(masked, secret) {
			t.Fatalf("redactor leaked %q in %s", secret, masked)
		}
	}
}

func TestExchangePathIsNotUnderV1(t *testing.T) {
	got := exchangeURL(DefaultAuthBase)
	if got != "https://www.orcarouter.ai/api/v1/auth/keys" {
		t.Fatalf("unexpected exchange URL: %s", got)
	}
	if strings.Contains(got, "api.orcarouter.ai") {
		t.Fatalf("exchange must never use the inference origin: %s", got)
	}
	if strings.Contains(got, "/v1/auth/keys") && !strings.Contains(got, "/api/v1/auth/keys") {
		t.Fatalf("exchange must be /api/v1/auth/keys: %s", got)
	}
}

func TestCredentialPayloadNeverCarriesClientSecret(t *testing.T) {
	payload, err := Credential{Format: CredentialFormatOAuth, APIKey: "sk-orca-NOSECRET000000000000", Scope: "api"}.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(payload), ClientSecretParam) {
		t.Fatalf("credential payload must not carry a client secret field: %s", payload)
	}
	var generic map[string]any
	if err := json.Unmarshal(payload, &generic); err != nil {
		t.Fatalf("payload must be JSON: %v", err)
	}
	if _, ok := generic["refresh_token"]; ok {
		t.Fatal("a durable OrcaRouter key has no refresh token")
	}
}
