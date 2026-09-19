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

// AllowsProvider answers the family-level grant question. The helpers live
// on accounts (Pool wrappers around grants.go) so this package does not
// import grant parsing and accounts does not import auth.
func (i Identity) AllowsProvider(provider string) bool {
	if strings.TrimSpace(provider) == "" {
		return true
	}
	return accounts.ProviderAllowed(provider, i.AllowedProviders)
}

// AllowsProviderRegion reports whether a concrete account — family plus
// region — is covered by the key allowlist. This is the fail-closed gate for
// routing candidates; AllowsProvider only answers the family-level question.
func (i Identity) AllowsProviderRegion(provider, region string) bool {
	if strings.TrimSpace(provider) == "" {
		return true
	}
	return accounts.ProviderRegionAllowed(provider, region, i.AllowedProviders)
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
