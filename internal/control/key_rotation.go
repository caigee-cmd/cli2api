package control

import (
	"context"
	"sync"
)

// KeyRotation serializes persistence, live authentication publication and worker reload.
type KeyRotation struct {
	Keys     *Keys
	Accounts *Accounts
	Mu       *sync.Mutex
	Generate func() (string, error)
	Publish  func(string)
}

func (s *KeyRotation) Rotate(ctx context.Context) (string, error) {
	s.Mu.Lock()
	defer s.Mu.Unlock()
	secret, err := s.Generate()
	if err != nil {
		return "", err
	}
	if err = s.Keys.SetConsoleSecret(ctx, proxyAPIKeySecret, secret); err != nil {
		return "", err
	}
	s.Publish(secret)
	if err = s.Accounts.ReplaceProxyAPIKey(ctx, secret); err != nil {
		return "", err
	}
	return secret, nil
}
