package workbuddy

import (
	"strings"
	"testing"
)

func TestDecodeCredentialCamelCase(t *testing.T) {
	payload := []byte(`{
		"uid":"u-camel",
		"nickname":"Cam",
		"domain":"codebuddy.cn",
		"accessToken":"at-camel",
		"refreshToken":"rt-camel",
		"expiresAt":4102444800,
		"enterpriseId":"e-camel"
	}`)
	credential, err := DecodeCredential(payload)
	if err != nil {
		t.Fatalf("DecodeCredential camelCase: %v", err)
	}
	if credential.AccessToken != "at-camel" || credential.RefreshToken != "rt-camel" ||
		credential.UID != "u-camel" || credential.EnterpriseID != "e-camel" ||
		credential.Nickname != "Cam" || credential.ExpiresAt != 4102444800 {
		t.Fatalf("credential=%+v", credential)
	}
	if !credential.Ready() {
		t.Fatal("camelCase credential with token and uid must be ready")
	}
}

func TestPrepareImportCamelCase(t *testing.T) {
	payload := []byte(`{"accessToken":"at","refreshToken":"rt","uid":"u1","domain":"codebuddy.cn","enterpriseId":"e1","nickname":"N","expiresAt":1}`)
	prepared, err := (credentialCodec{}).PrepareImport(payload)
	if err != nil {
		t.Fatalf("PrepareImport: %v", err)
	}
	if !prepared.Ready {
		t.Fatal("expected ready")
	}
	decoded, err := DecodeCredential(prepared.Payload)
	if err != nil || decoded.AccessToken != "at" || decoded.UID != "u1" || decoded.EnterpriseID != "e1" {
		t.Fatalf("canonical=%+v err=%v", decoded, err)
	}
	// Stored payload must be snake_case canonical.
	if strings.Contains(string(prepared.Payload), "accessToken") {
		t.Fatalf("expected canonical snake_case payload, got %s", prepared.Payload)
	}
}

func TestExpandImportWorkbuddyAccountsSingle(t *testing.T) {
	payload := []byte(`{
		"format":"workbuddy-accounts",
		"version":1,
		"count":1,
		"accounts":[{
			"uid":"u1",
			"nickname":"One",
			"domain":"codebuddy.cn",
			"accessToken":"at1",
			"refreshToken":"rt1",
			"expiresAt":4102444800,
			"enterpriseId":"e1"
		}]
	}`)
	items, err := ExpandImport(payload)
	if err != nil {
		t.Fatalf("ExpandImport: %v", err)
	}
	if len(items) != 1 || !items[0].Ready {
		t.Fatalf("items=%+v", items)
	}
	decoded, err := DecodeCredential(items[0].Payload)
	if err != nil || decoded.UID != "u1" || decoded.AccessToken != "at1" {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
}

func TestExpandImportWorkbuddyAccountsMany(t *testing.T) {
	payload := []byte(`{
		"format":"workbuddy-accounts",
		"version":1,
		"count":2,
		"accounts":[
			{"uid":"u1","nickname":"One","domain":"codebuddy.cn","accessToken":"at1","refreshToken":"rt1","expiresAt":1,"enterpriseId":"e1"},
			{"uid":"u2","nickname":"Two","domain":"workbuddy.ai","accessToken":"at2","refreshToken":"rt2","expiresAt":2,"enterpriseId":"e2"}
		]
	}`)
	items, err := ExpandImport(payload)
	if err != nil {
		t.Fatalf("ExpandImport: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len=%d", len(items))
	}
	first, _ := DecodeCredential(items[0].Payload)
	second, _ := DecodeCredential(items[1].Payload)
	if first.UID != "u1" || second.UID != "u2" || second.AccessToken != "at2" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestExpandImportRejectsBadEntry(t *testing.T) {
	payload := []byte(`{
		"format":"workbuddy-accounts",
		"accounts":[
			{"uid":"u1","accessToken":"at1","refreshToken":"rt1"},
			{"uid":"u2","nickname":"missing-token"}
		]
	}`)
	_, err := ExpandImport(payload)
	if err == nil || !strings.Contains(err.Error(), "accounts[1]") {
		t.Fatalf("expected accounts[1] error, got %v", err)
	}
}

func TestPrepareImportRejectsMultiAccountPackage(t *testing.T) {
	payload := []byte(`{
		"format":"workbuddy-accounts",
		"accounts":[
			{"uid":"u1","accessToken":"at1"},
			{"uid":"u2","accessToken":"at2"}
		]
	}`)
	_, err := (credentialCodec{}).PrepareImport(payload)
	if err == nil || !strings.Contains(err.Error(), "batch import") {
		t.Fatalf("err=%v", err)
	}
}

func TestDecodeCredentialNestedAndSnakeStillWork(t *testing.T) {
	nested := []byte(`{"account":{"uid":"u-n","enterpriseId":"e-n","nickname":"Nest"},"auth":{"accessToken":"at-n","refreshToken":"rt-n","expiresAt":9,"domain":"codebuddy.cn"}}`)
	credential, err := DecodeCredential(nested)
	if err != nil || credential.UID != "u-n" || credential.AccessToken != "at-n" {
		t.Fatalf("nested=%+v err=%v", credential, err)
	}
	snake := []byte(`{"access_token":"at-s","refresh_token":"rt-s","expires_at":8,"domain":"codebuddy.cn","uid":"u-s","enterprise_id":"e-s","nickname":"Snake"}`)
	credential, err = DecodeCredential(snake)
	if err != nil || credential.UID != "u-s" || credential.AccessToken != "at-s" {
		t.Fatalf("snake=%+v err=%v", credential, err)
	}
}
