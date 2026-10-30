package codearts

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// dpopKeyPair is the ES256 key pair used in the DPoP proof sent to STS.
type dpopKeyPair struct {
	PrivateKey *ecdsa.PrivateKey
	PublicJWK  map[string]string
}

// newDPoPPrivateJWK generates a persistable P-256 private key. STS binds the
// refresh token to the public half, so the same JWK must be reused on refresh.
func newDPoPPrivateJWK() (map[string]string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"kty": "EC",
		"crv": "P-256",
		"x":   base64.RawURLEncoding.EncodeToString(padded(key.PublicKey.X, 32)),
		"y":   base64.RawURLEncoding.EncodeToString(padded(key.PublicKey.Y, 32)),
		"d":   base64.RawURLEncoding.EncodeToString(padded(key.D, 32)),
	}, nil
}

func dpopKeyPairFor(jwk map[string]string) (*dpopKeyPair, error) {
	if len(jwk) == 0 || jwk["d"] == "" {
		generated, err := newDPoPPrivateJWK()
		if err != nil {
			return nil, err
		}
		jwk = generated
	}
	if jwk["kty"] != "EC" || jwk["crv"] != "P-256" {
		return nil, fmt.Errorf("invalid DPoP JWK curve")
	}
	decode := func(field string) ([]byte, error) {
		raw, err := base64.RawURLEncoding.DecodeString(jwk[field])
		if err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("invalid DPoP JWK %s", field)
		}
		return raw, nil
	}
	xRaw, err := decode("x")
	if err != nil {
		return nil, err
	}
	yRaw, err := decode("y")
	if err != nil {
		return nil, err
	}
	dRaw, err := decode("d")
	if err != nil {
		return nil, err
	}
	curve := elliptic.P256()
	x, y, d := new(big.Int).SetBytes(xRaw), new(big.Int).SetBytes(yRaw), new(big.Int).SetBytes(dRaw)
	if d.Sign() <= 0 || d.Cmp(curve.Params().N) >= 0 || !curve.IsOnCurve(x, y) {
		return nil, fmt.Errorf("invalid DPoP JWK key material")
	}
	wantX, wantY := curve.ScalarBaseMult(dRaw)
	if wantX.Cmp(x) != 0 || wantY.Cmp(y) != 0 {
		return nil, fmt.Errorf("DPoP JWK public/private key mismatch")
	}
	return &dpopKeyPair{
		PrivateKey: &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y}, D: d},
		PublicJWK:  map[string]string{"kty": "EC", "crv": "P-256", "x": jwk["x"], "y": jwk["y"]},
	}, nil
}

// signDPoPProof builds the RFC 9449 proof for the token endpoint.
func signDPoPProof(kp *dpopKeyPair, htu string) (string, error) {
	header, _ := json.Marshal(map[string]any{
		"alg": "ES256",
		"typ": "dpop+jwt",
		"jwk": kp.PublicJWK,
	})
	payload, _ := json.Marshal(map[string]any{
		"htm": "POST",
		"htu": htu,
		"iat": time.Now().Unix(),
		"jti": randomHex(32),
	})
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, kp.PrivateKey, digest[:])
	if err != nil {
		return "", err
	}
	n := elliptic.P256().Params().N
	halfN := new(big.Int).Rsh(n, 1)
	if s.Cmp(halfN) > 0 {
		s.Sub(n, s)
	}
	sig := append(padded(r, 32), padded(s, 32)...)
	return input + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func padded(b *big.Int, size int) []byte {
	out := make([]byte, size)
	raw := b.Bytes()
	copy(out[size-len(raw):], raw)
	return out
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}
