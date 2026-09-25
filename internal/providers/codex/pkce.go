package codex

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// pkceVerifier returns a fresh RFC 7636 code verifier.
func pkceVerifier() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// pkceChallenge derives the S256 challenge for a verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func pkcePair() (verifier, challenge string, err error) {
	verifier, err = pkceVerifier()
	if err != nil {
		return "", "", err
	}
	if verifier == "" {
		return "", "", errors.New("empty pkce verifier")
	}
	return verifier, pkceChallenge(verifier), nil
}
