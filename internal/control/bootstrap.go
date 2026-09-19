package control

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

type SecretStore interface {
	GetSecret(ctx context.Context, name string) (string, bool, error)
	SetSecret(ctx context.Context, name, value string) error
}

const proxyAPIKeySecret = "proxy_api_key"

func EnsureProxyAPIKey(ctx context.Context, store SecretStore, bootstrap string) (string, bool, error) {
	if value, ok, err := store.GetSecret(ctx, proxyAPIKeySecret); err != nil {
		return "", false, err
	} else if ok && strings.TrimSpace(value) != "" {
		return value, false, nil
	}

	key := strings.TrimSpace(bootstrap)
	if key == "" || key == "change-me" || key == "dev-key" {
		generated, err := GenerateAPIKey()
		if err != nil {
			return "", false, err
		}
		key = generated
	}
	if err := store.SetSecret(ctx, proxyAPIKeySecret, key); err != nil {
		return "", false, err
	}
	return key, true, nil
}

func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate proxy api key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
