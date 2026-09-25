package command

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// identityProfile is the subset of GET /alpha/whoami this adapter keeps.
type identityProfile struct {
	UserID string
	Email  string
	Name   string
}

// fetchWhoami validates the key and returns identity metadata. A 200 means the
// key resolves; anything else is classified.
func (c *Client) fetchWhoami(ctx context.Context, client *http.Client, credential Credential) (identityProfile, error) {
	body, status, err := c.getJSON(ctx, client, credential, PathWhoami)
	if err != nil {
		return identityProfile{}, err
	}
	if status >= 300 {
		return identityProfile{}, newProviderError(status, string(body))
	}
	var parsed struct {
		Success bool `json:"success"`
		User    struct {
			ID       string `json:"id"`
			UserID   string `json:"userId"`
			Email    string `json:"email"`
			Name     string `json:"name"`
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return identityProfile{}, err
	}
	return identityProfile{
		UserID: firstNonEmpty(parsed.User.ID, parsed.User.UserID),
		Email:  strings.TrimSpace(parsed.User.Email),
		Name:   firstNonEmpty(parsed.User.Name, parsed.User.Username),
	}, nil
}

// windowLimit is one rolling usage window from the credits response's
// root-level windowLimits object. used/cap share the credit unit; resetAt is a
// millisecond epoch.
type windowLimit struct {
	Used    float64 `json:"used"`
	Cap     float64 `json:"cap"`
	ResetAt int64   `json:"resetAt"`
}

// credits is the GET /alpha/billing/credits balance plus the rolling windows.
type credits struct {
	Monthly   float64
	Purchased float64
	Free      float64
	PlanID    string
	FiveHour  *windowLimit
	Weekly    *windowLimit
}

func (cr credits) total() float64 { return cr.Monthly + cr.Purchased + cr.Free }

// planMonthlyCredits maps a planId to its monthly credit allowance, mirroring
// the CLI's own plan table. The 5-hour/weekly caps arrive in windowLimits; the
// monthly window has to be derived from this allowance minus the remaining
// monthly credits. Unknown plans leave the monthly window unset rather than
// guessing a total.
var planMonthlyCredits = map[string]float64{
	"individual-go":       10,
	"individual-goat":     70,
	"individual-pro":      30,
	"individual-pro-v1":   80,
	"individual-provider": 15,
	"individual-max":      150,
	"individual-ultra":    300,
	"teams-pro":           40,
}

func planTotalCredits(planID string) (float64, bool) {
	norm := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(planID), "_", "-"))
	if norm == "" {
		return 0, false
	}
	best := ""
	for key := range planMonthlyCredits {
		if strings.HasPrefix(norm, key) && len(key) > len(best) {
			best = key
		}
	}
	if best == "" {
		return 0, false
	}
	return planMonthlyCredits[best], true
}

// fetchCredits reads the account credit balance and rolling windows. Callers
// treat a failure as "quota unknown", never as a readiness signal.
func (c *Client) fetchCredits(ctx context.Context, client *http.Client, credential Credential) (credits, error) {
	body, status, err := c.getJSON(ctx, client, credential, PathBillingCredits)
	if err != nil {
		return credits{}, err
	}
	if status >= 300 {
		return credits{}, newProviderError(status, string(body))
	}
	var parsed struct {
		Credits struct {
			MonthlyCredits   float64 `json:"monthlyCredits"`
			PurchasedCredits float64 `json:"purchasedCredits"`
			FreeCredits      float64 `json:"freeCredits"`
			PlanID           string  `json:"planId"`
			// Fallback location: the CLI reads windowLimits at the response root,
			// but accept a nested copy so a shape move does not blank the meter.
			WindowLimits struct {
				FiveHour *windowLimit `json:"fiveHour"`
				Weekly   *windowLimit `json:"weekly"`
			} `json:"windowLimits"`
		} `json:"credits"`
		WindowLimits struct {
			FiveHour *windowLimit `json:"fiveHour"`
			Weekly   *windowLimit `json:"weekly"`
		} `json:"windowLimits"`
		PlanID string `json:"planId"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return credits{}, err
	}
	fiveHour := parsed.WindowLimits.FiveHour
	if fiveHour == nil {
		fiveHour = parsed.Credits.WindowLimits.FiveHour
	}
	weekly := parsed.WindowLimits.Weekly
	if weekly == nil {
		weekly = parsed.Credits.WindowLimits.Weekly
	}
	return credits{
		Monthly:   parsed.Credits.MonthlyCredits,
		Purchased: parsed.Credits.PurchasedCredits,
		Free:      parsed.Credits.FreeCredits,
		PlanID:    firstNonEmpty(parsed.Credits.PlanID, parsed.PlanID),
		FiveHour:  fiveHour,
		Weekly:    weekly,
	}, nil
}

func (c *Client) getJSON(ctx context.Context, client *http.Client, credential Credential, path string) ([]byte, int, error) {
	if client == nil {
		client = c.http
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointBase(credential)+path, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-cli-environment", CLIEnvironment)
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}
