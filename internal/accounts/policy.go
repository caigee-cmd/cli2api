package accounts

import (
	"fmt"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/proxy"
)

const (
	DefaultMaxInFlight = 4
	DefaultPriority    = 50
)

func DefaultMaxInFlightValue(value int) int {
	if value <= 0 {
		return DefaultMaxInFlight
	}
	return value
}

func DefaultPriorityValue(value int) int {
	if value <= 0 {
		return DefaultPriority
	}
	return value
}

// DefaultDropSystemPrompt is the create-time default. WorkBuddy strips caller
// system prompts because upstream content screening rejects third-party
// identity. Trae and Kimi keep them: clients such as MiMo Code put JSON
// instructions in the system prompt, and those families have no equivalent
// screening. An explicit value always wins.
func DefaultDropSystemPrompt(provider string, value *bool) bool {
	if value != nil {
		return *value
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "trae", "kimi":
		return false
	default:
		return true
	}
}

func DefaultWorkBuddyAutoCheckin(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func ValidateModelContextLength(contextLength int) error {
	if contextLength < 0 || contextLength > 4_000_000 || (contextLength > 0 && contextLength < 1024) {
		return fmt.Errorf("context_length must be 0 or between 1024 and 4000000")
	}
	return nil
}

// ResolveWorkBuddyCheckinTime inherits defaultTime when value is empty.
func ResolveWorkBuddyCheckinTime(value, defaultTime string) (string, error) {
	if strings.TrimSpace(value) == "" {
		if strings.TrimSpace(defaultTime) == "" {
			return DefaultWorkBuddyCheckinTime, nil
		}
		return defaultTime, nil
	}
	return NormalizeWorkBuddyCheckinTime(value)
}

// ValidateAccountProxy enforces the per-provider proxy boundary. Child-process
// providers (Qoder) can only forward every cloud request through an http(s)
// proxy, so SOCKS is rejected for them; in-process providers (WorkBuddy, Trae)
// may use any scheme Parse accepts.
func ValidateAccountProxy(providerID, region, raw string) error {
	descriptor, _, err := providers.Resolve(providerID, region)
	if err != nil {
		return err
	}
	if descriptor.Runtime == providers.RuntimeChildProcess {
		if err := proxy.ValidateHTTPOnly(raw); err != nil {
			return fmt.Errorf("Qoder account proxy: %w", err)
		}
		return nil
	}
	_, err = proxy.Parse(raw)
	return err
}
