package auth

import (
	"context"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

const (
	KindNone    = "none"
	KindConsole = "console"
	KindKey     = "key"
)

type Identity struct {
	Kind             string
	KeyID            string
	Name             string
	AllowedProviders []string
}

func (i Identity) Console() bool {
	return i.Kind == KindNone || i.Kind == KindConsole
}

func (i Identity) AllowsProvider(provider string) bool {
	if strings.TrimSpace(provider) == "" {
		return true
	}
	return accounts.ProviderAllowed(provider, i.AllowedProviders)
}

type ctxKey struct{}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, identity)
}

func IdentityFrom(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(ctxKey{}).(Identity)
	return identity, ok
}

func ConsoleIdentity() Identity {
	return Identity{Kind: KindConsole, Name: "console"}
}

func KeyIdentity(key accounts.APIKey) Identity {
	return Identity{
		Kind:             KindKey,
		KeyID:            key.ID,
		Name:             key.Name,
		AllowedProviders: append([]string{}, key.Providers...),
	}
}
