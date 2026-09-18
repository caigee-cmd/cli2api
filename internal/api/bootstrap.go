package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

type secretStore interface {
	GetSecret(ctx context.Context, name string) (string, bool, error)
	SetSecret(ctx context.Context, name, value string) error
}

const proxyAPIKeySecret = "proxy_api_key"

func ensureProxyAPIKey(ctx context.Context, store secretStore, bootstrap string) (string, bool, error) {
	if value, ok, err := store.GetSecret(ctx, proxyAPIKeySecret); err != nil {
		return "", false, err
	} else if ok && strings.TrimSpace(value) != "" {
		return value, false, nil
	}

	key := strings.TrimSpace(bootstrap)
	if key == "" || key == "change-me" || key == "dev-key" {
		generated, err := generateAPIKey()
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

func generateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate proxy api key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
