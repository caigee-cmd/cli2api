package qoder

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestEncodeBodyRoundTrip(t *testing.T) {
	plain := []byte(`{"hello":"qoder","n":1}`) // length forces base64 padding
	encoded := EncodeBody(plain)
	if strings.Contains(encoded, "A") && strings.Contains(encoded, "B") && encoded == base64.StdEncoding.EncodeToString(plain) {
		t.Fatal("encoded body must not be standard base64")
	}
	if strings.Contains(encoded, "=") || !strings.Contains(encoded, "$") {
		t.Fatalf("padding must use $ not =: %s", encoded)
	}
	got, err := DecodeBody(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("round trip = %q", got)
	}
}

func TestDecryptCLIUserPlainAndCipher(t *testing.T) {
	plain := []byte(`{"uid":"u-1","name":"Ada","access_token":"dt-token","refresh_token":"drt-token"}`)
	user, err := DecryptCLIUser(plain, "machine-id-is-long-enough")
	if err != nil {
		t.Fatal(err)
	}
	if user.uid() != "u-1" || user.token() != "dt-token" {
		t.Fatalf("plain user = %+v", user)
	}

	machineID := "0123456789abcdef-rest"
	encrypted, err := aesCBCEncrypt(plain, []byte(machineID[:16]), []byte(machineID[:16]))
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte(base64.StdEncoding.EncodeToString(encrypted))
	user, err = DecryptCLIUser(blob, machineID)
	if err != nil {
		t.Fatal(err)
	}
	if user.Name != "Ada" || user.RefreshToken != "drt-token" {
		t.Fatalf("cipher user = %+v", user)
	}
	if _, err := DecryptCLIUser(blob, "short"); err == nil {
		t.Fatal("short machine id must fail")
	}
}

func TestSessionSignIsStableForMachineAndCoversBody(t *testing.T) {
	user := CLIUser{UID: "uid-9", Name: "Ada", AccessToken: "dt-1", RefreshToken: "drt-1"}
	a, err := NewSession(user)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSession(user)
	if err != nil {
		t.Fatal(err)
	}
	if a.MachineID != b.MachineID || a.MachineToken != b.MachineToken || a.MachineType != b.MachineType {
		t.Fatalf("machine drift a=%+v b=%+v", a, b)
	}
	if a.MachineID == "" || len(a.MachineType) != 18 || len(a.MachineToken) != 43 {
		t.Fatalf("machine shape id=%q type=%q token=%q", a.MachineID, a.MachineType, a.MachineToken)
	}
	endpoint := ChatEndpoint("global")
	left, err := a.Sign("body-one", endpoint, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	right, err := a.Sign("body-two", endpoint, time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	if left["Authorization"] == right["Authorization"] {
		t.Fatal("signature must cover the body")
	}
	if !strings.HasPrefix(left["Authorization"], "Bearer COSY.") {
		t.Fatalf("authorization = %q", left["Authorization"])
	}
	if left["Cosy-Version"] != COSYVersion || left["Cosy-User"] != "uid-9" {
		t.Fatalf("headers = %#v", left)
	}
	if !strings.Contains(endpoint, "api1.qoder.sh") || !strings.Contains(ChatEndpoint("cn"), "gateway.qoder.com.cn") {
		t.Fatalf("endpoints global=%s cn=%s", endpoint, ChatEndpoint("cn"))
	}
}

func TestAESBlockRoundTripUsesMachineKey(t *testing.T) {
	key := []byte("0123456789abcdef")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("hello qoder user")
	enc, err := aesCBCEncrypt(plain, key, key)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(enc))
	cipher.NewCBCDecrypter(block, key).CryptBlocks(out, enc)
	got, err := pkcs7Unpad(out, block.BlockSize())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("got %q", got)
	}
}
