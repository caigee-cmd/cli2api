package trae

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
)

func TestSignDeviceProofMatchesOfficialPayload(t *testing.T) {
	privPEM, _, err := generateDeviceKey()
	if err != nil {
		t.Fatal(err)
	}
	payload := deviceProofPayload(pathExchangeCode, ClientID, "secret-refresh-token", 1750000000, "abc123")
	want := "POST\n/trae/api/v3/oauth/ExchangeToken\n" + ClientID + "\nsecret-refresh-token\n1750000000\nabc123"
	if payload != want {
		t.Fatalf("payload=%q want %q", payload, want)
	}
	encoded, err := signDeviceProof(privPEM, payload)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := parseDeviceKey(privPEM)
	if err != nil {
		t.Fatal(err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.ParsePKIXPublicKey(publicDER)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(payload))
	if !ecdsa.VerifyASN1(public.(*ecdsa.PublicKey), digest[:], signature) {
		t.Fatal("signature does not verify with the registered device public key")
	}
	if len(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})) == 0 {
		t.Fatal("public key must use SPKI PEM")
	}
}
