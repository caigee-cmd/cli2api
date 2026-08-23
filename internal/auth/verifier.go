package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

type Verifier struct {
	key string
}

func NewVerifier(key string) Verifier {
	return Verifier{key: strings.TrimSpace(key)}
}

func (v Verifier) Authorized(r *http.Request) bool {
	if v.key == "" {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" {
		got = r.Header.Get("x-api-key")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(v.key)) == 1
}
