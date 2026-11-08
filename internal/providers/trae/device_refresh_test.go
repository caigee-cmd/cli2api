package trae

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestExchangeTokenUsesDeviceBoundRenewal(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != pathExchangeCode {
			t.Fatalf("path=%s, want device-bound exchange", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Cloud-IDE-JWT old-at" {
			t.Fatalf("Authorization=%q", got)
		}
		var request struct {
			ClientID     string
			RefreshToken string
			DeviceInfo   map[string]any
			DeviceProof  map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ClientID != ClientID || request.RefreshToken != "rt" {
			t.Fatalf("unexpected identity: client=%q", request.ClientID)
		}
		if request.DeviceInfo["PlatformCode"] != "IDE_PC" || request.DeviceInfo["DeviceID"] != "1234567890123456" {
			t.Fatalf("DeviceInfo=%v", request.DeviceInfo)
		}
		if request.DeviceInfo["DevicePublicKey"] == "" {
			t.Fatal("device public key is missing")
		}
		timestamp, ok := request.DeviceProof["Timestamp"].(float64)
		if !ok {
			t.Fatalf("Timestamp=%T", request.DeviceProof["Timestamp"])
		}
		nonce, _ := request.DeviceProof["Nonce"].(string)
		signature, _ := request.DeviceProof["Signature"].(string)
		payloadToVerify := deviceProofPayload(pathExchangeCode, ClientID, "rt", int64(timestamp), nonce)
		signatureDER, err := base64.StdEncoding.DecodeString(signature)
		if err != nil {
			t.Fatalf("signature is not base64: %v", err)
		}
		block, _ := pem.Decode([]byte(request.DeviceInfo["DevicePublicKey"].(string)))
		public, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256([]byte(payloadToVerify))
		if !ecdsa.VerifyASN1(public.(*ecdsa.PublicKey), digest[:], signatureDER) {
			t.Fatal("DeviceProof does not verify against DeviceInfo.DevicePublicKey")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
			"Token": "device-bound-at", "TokenExpireAt": 4102444800,
		}})
	}))
	credential, err := client.ExchangeToken(context.Background(), "acc1", Credential{
		AccessToken: "old-at", RefreshToken: "rt", UID: "u1",
		MachineID: "machine", DeviceID: "1234567890123456",
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "device-bound-at" || credential.RefreshToken != "rt" {
		t.Fatalf("credential=%+v", credential)
	}
	if credential.DevicePrivateKey == "" {
		t.Fatal("generated device key was not persisted")
	}
}

func TestExchangeTokenFallsBackWhenDeviceRenewalRejected(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathExchangeCode:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"code":10101,"message":"refresh token is not matched to the client"}`)
		case pathExchange:
			_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{
				"Token": "legacy-at", "TokenExpireAt": 4102444800, "RefreshToken": "rotated-rt",
			}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	credential, err := client.ExchangeToken(context.Background(), "acc1", Credential{
		RefreshToken: "rt", AccessToken: "old-at",
	})
	if err != nil {
		t.Fatal(err)
	}
	if credential.AccessToken != "legacy-at" || credential.RefreshToken != "rotated-rt" {
		t.Fatalf("credential=%+v", credential)
	}
}

func TestExchangeDeviceBoundUsesLegacySoloIdentity(t *testing.T) {
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ClientID    string
			IDEVersion  string
			DeviceInfo  map[string]any
			DeviceProof map[string]any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ClientID != LegacyClientID || request.IDEVersion != "0.1.64" || request.DeviceInfo["PlatformCode"] != "SOLO_PC" {
			t.Fatalf("legacy identity mismatch: client=%q version=%q platform=%v", request.ClientID, request.IDEVersion, request.DeviceInfo["PlatformCode"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Result": map[string]any{"Token": "solo-at", "TokenExpireAt": 4102444800}})
	}))
	credential, err := client.ExchangeToken(context.Background(), "acc1", Credential{
		RefreshToken: "rt", RefreshClientID: LegacyClientID,
	})
	if err != nil || !strings.HasPrefix(credential.AccessToken, "solo-") {
		t.Fatalf("credential=%+v err=%v", credential, err)
	}
}
