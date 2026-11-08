package trae

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
)

// generateDeviceKey creates an EC P-256 keypair for the "device proof" the v3
// code exchange requires. The public key is sent as a PEM SubjectPublicKeyInfo
// block (the shape the client sends as DeviceInfo.DevicePublicKey); the private
// key is stored in the credential so the public key can be re-derived later.
func generateDeviceKey() (privPEM, pubPEM string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	privPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return "", "", err
	}
	pubPEM = string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}))
	return privPEM, pubPEM, nil
}

func parseDeviceKey(privPEM string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privPEM))
	if block == nil {
		return nil, fmt.Errorf("device key: not PEM")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if ec, ok := key.(*ecdsa.PrivateKey); ok {
			return ec, nil
		}
	}
	if ec, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return ec, nil
	}
	return nil, fmt.Errorf("device key: not an EC private key")
}

// devicePublicKeyPEM returns the PEM public key for a stored private key.
func devicePublicKeyPEM(privPEM string) string {
	priv, err := parseDeviceKey(privPEM)
	if err != nil {
		return ""
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// deviceInfo is the single DeviceInfo shape sent on login, code exchange and
// renewal. The server binds the account to the device named here and rejects a
// later request whose fingerprint disagrees, so every caller must build it from
// this one function.
func deviceInfo(credential Credential, platformCode, clientVersion string) map[string]any {
	info := map[string]any{
		"DeviceID":      credential.DeviceID,
		"MachineID":     credential.MachineID,
		"PlatformCode":  platformCode,
		"DeviceType":    DeviceType,
		"DeviceName":    "",
		"DeviceModel":   "",
		"ClientVersion": clientVersion,
		"DeviceBrand":   DeviceBrand,
		"DeviceCPU":     DeviceCPU,
		"OSInfo":        DeviceOSInfo,
		"OSVersion":     DeviceOSBuild,
	}
	if pubKey := devicePublicKeyPEM(credential.DevicePrivateKey); pubKey != "" {
		info["DevicePublicKey"] = pubKey
	}
	return info
}

// deviceProofPayload is the exact text signed by the official client when it
// renews a device-bound token. Refresh tokens must not be logged or included
// in errors; this value is only passed to the signer.
func deviceProofPayload(path, clientID, refreshToken string, timestamp int64, nonce string) string {
	return strings.Join([]string{"POST", path, clientID, refreshToken, fmt.Sprintf("%d", timestamp), nonce}, "\n")
}

// signDeviceProof returns the client's ECDSA-SHA256 signature in the ASN.1 DER
// form accepted by ExchangeToken. The device key is the same P-256 pair whose
// public half was registered in DeviceInfo during login.
func signDeviceProof(privPEM, payload string) (string, error) {
	priv, err := parseDeviceKey(privPEM)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(payload))
	signature, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign device proof: %w", err)
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}
