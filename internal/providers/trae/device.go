package trae

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
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
