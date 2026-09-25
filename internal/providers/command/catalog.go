package command

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// CatalogEntry is one model from GET /provider/v1/models. The endpoint is
// readable anonymously; supported_endpoints is recorded but not used for
// routing because /alpha/generate (the path this adapter always speaks) serves
// every model regardless of the declared endpoints.
type CatalogEntry struct {
	ID                 string   `json:"id"`
	Object             string   `json:"object"`
	Name               string   `json:"name"`
	ContextLength      int      `json:"context_length"`
	SupportedEndpoints []string `json:"supported_endpoints"`
}

type catalogEnvelope struct {
	Object string         `json:"object"`
	Data   []CatalogEntry `json:"data"`
}

const catalogTTL = 3 * time.Hour

// ParseCatalogJSON maps the /provider/v1/models payload onto the provider
// ModelInfo surface. Native and public ids are the gateway's canonical ids
// (e.g. "deepseek/deepseek-v4-pro"); they are distinct only in case. Reasoning
// support is deliberately not claimed: the catalog does not declare it, and
// inventing levels a model does not declare is disallowed. Reasoning wiring is
// a follow-up.
func ParseCatalogJSON(raw []byte) ([]providers.ModelInfo, error) {
	var env catalogEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return nil, fmt.Errorf("command catalog empty")
	}
	models := make([]providers.ModelInfo, 0, len(env.Data))
	seen := map[string]struct{}{}
	for _, entry := range env.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		caps := providers.ModelCapabilities{
			ContextWindow: entry.ContextLength,
			Tools:         true,
		}
		models = append(models, providers.ModelInfo{
			NativeModel:  id,
			PublicModel:  id,
			DisplayName:  firstNonEmpty(entry.Name, id),
			Capabilities: applyReasoningCaps(id, caps),
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("command catalog produced no models")
	}
	return models, nil
}

// fetchCatalog retrieves and parses the live model list.
func (c *Client) fetchCatalog(ctx context.Context, credential Credential) ([]providers.ModelInfo, error) {
	client, err := c.httpClient(ctx, "")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpointBase(credential)+PathModels, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-cli-environment", CLIEnvironment)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, newProviderError(resp.StatusCode, string(body))
	}
	return ParseCatalogJSON(body)
}

// Models implements providers.ModelCatalogProvider. It serves the TTL cache when
// warm and refreshes from upstream otherwise. The catalog is provider-level
// (identical across accounts), so one cache backs every account.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	if models, ok := c.cachedModels(time.Now()); ok {
		return models, nil
	}
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	models, err := c.fetchCatalog(ctx, credential)
	if err != nil {
		return nil, err
	}
	c.rememberModels(models, time.Now())
	return cloneModels(models), nil
}

func (c *Client) cachedModels(now time.Time) ([]providers.ModelInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.catalog) == 0 || c.catalogAt.IsZero() || now.Sub(c.catalogAt) > catalogTTL {
		return nil, false
	}
	return cloneModels(c.catalog), true
}

func (c *Client) rememberModels(models []providers.ModelInfo, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.catalog = cloneModels(models)
	c.catalogAt = at
}

func (c *Client) lookupModel(model string) (providers.ModelInfo, bool) {
	want := strings.ToLower(strings.TrimSpace(model))
	if want == "" {
		return providers.ModelInfo{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, info := range c.catalog {
		if strings.ToLower(info.NativeModel) == want || strings.ToLower(info.PublicModel) == want {
			return info, true
		}
	}
	return providers.ModelInfo{}, false
}

func cloneModels(in []providers.ModelInfo) []providers.ModelInfo {
	if len(in) == 0 {
		return nil
	}
	out := make([]providers.ModelInfo, len(in))
	for i, m := range in {
		out[i] = m
		if len(m.Capabilities.ReasoningOptions) > 0 {
			out[i].Capabilities.ReasoningOptions = append([]string(nil), m.Capabilities.ReasoningOptions...)
		}
	}
	return out
}
