package codearts

import (
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// modelAvailabilityTTL is how long a learned per-account model verdict is kept.
// CodeArts catalogs list models an account cannot actually call, so the adapter
// learns from real chat traffic and filters the catalog after the first 404.
const modelAvailabilityTTL = 30 * time.Minute

type modelAvailability struct {
	unusable bool
	at       time.Time
}

func availabilityKey(accountID, model string) string {
	return accountID + "|" + strings.ToLower(strings.TrimSpace(model))
}

func (c *Client) markModelUnusable(accountID, model string) {
	if accountID == "" || strings.TrimSpace(model) == "" {
		return
	}
	c.availMu.Lock()
	defer c.availMu.Unlock()
	if c.availability == nil {
		c.availability = map[string]modelAvailability{}
	}
	c.availability[availabilityKey(accountID, model)] = modelAvailability{unusable: true, at: c.now()}
}

func (c *Client) markModelUsable(accountID, model string) {
	if accountID == "" || strings.TrimSpace(model) == "" {
		return
	}
	c.availMu.Lock()
	defer c.availMu.Unlock()
	if c.availability == nil {
		c.availability = map[string]modelAvailability{}
	}
	c.availability[availabilityKey(accountID, model)] = modelAvailability{at: c.now()}
}

func (c *Client) modelUnusable(accountID, model string) bool {
	c.availMu.Lock()
	defer c.availMu.Unlock()
	entry, ok := c.availability[availabilityKey(accountID, model)]
	if !ok || c.now().Sub(entry.at) > modelAvailabilityTTL {
		return false
	}
	return entry.unusable
}

// modelUnavailableBody recognizes the two upstream verdicts that mean "this
// account cannot call this model", as opposed to an auth or transport failure.
func modelUnavailableBody(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{
		"inferhub.002002009", "not registered", "4004.200", "benefit not found",
		"model is not available", "model not found",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// filterAvailable removes models the account has already been told it cannot
// call. The catalog itself is not rewritten so a later benefit claim can make
// the model visible again.
func (c *Client) filterAvailable(accountID string, models []providers.ModelInfo) []providers.ModelInfo {
	out := make([]providers.ModelInfo, 0, len(models))
	for _, model := range models {
		if c.modelUnusable(accountID, model.NativeModel) || c.modelUnusable(accountID, model.PublicModel) {
			continue
		}
		out = append(out, model)
	}
	return out
}
