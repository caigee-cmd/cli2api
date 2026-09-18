package executor

import (
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// QuotaSnapshot stays an accounts type so store and Account can share it
// without importing executor. The alias keeps pool field types local.
type QuotaSnapshot = accounts.QuotaSnapshot

const (
	KindQuota             = accounts.KindQuota
	KindRateLimit         = accounts.KindRateLimit
	KindAuth              = accounts.KindAuth
	KindNotReady          = accounts.KindNotReady
	KindUnavailable       = accounts.KindUnavailable
	KindInvalidRequest    = accounts.KindInvalidRequest
	KindModelNotAvailable = accounts.KindModelNotAvailable
	KindCanceled          = accounts.KindCanceled
	BackoffMaxLevel       = accounts.BackoffMaxLevel

	RoutingStrategyRoundRobin         = accounts.RoutingStrategyRoundRobin
	RoutingStrategyWeightedRoundRobin = accounts.RoutingStrategyWeightedRoundRobin
	RoutingStrategyFillFirst          = accounts.RoutingStrategyFillFirst
)

func NormalizeRoutingStrategy(strategy string) string {
	return accounts.NormalizeRoutingStrategy(strategy)
}

func NormalizeProviderFamily(provider string) string {
	return accounts.NormalizeProviderFamily(provider)
}

func NormalizeRegion(region string) string {
	return accounts.NormalizeRegion(region)
}

func CanonicalModelID(model string) string {
	return accounts.CanonicalModelID(model)
}

func NormalizeModelName(model string) string {
	return accounts.NormalizeModelName(model)
}

func NormalizeWeight(priority int) int {
	return accounts.NormalizeWeight(priority)
}

func ClampBackoffLevel(level int) int {
	return accounts.ClampBackoffLevel(level)
}

func ProviderAllowed(provider string, allowed []string) bool {
	return accounts.ProviderAllowed(provider, allowed)
}

func ProviderRegionAllowed(provider, region string, allowed []string) bool {
	return accounts.ProviderRegionAllowed(provider, region, allowed)
}

func IsPromptLimitText(text string) bool {
	return accounts.IsPromptLimitText(text)
}

func IsInvalidRequestText(text string) bool {
	return accounts.IsInvalidRequestText(text)
}

func NextLocalMidnightCooldown() time.Duration {
	return accounts.NextLocalMidnightCooldown()
}

func NextLocalMidnightCooldownAt(now time.Time) time.Duration {
	return accounts.NextLocalMidnightCooldownAt(now)
}
