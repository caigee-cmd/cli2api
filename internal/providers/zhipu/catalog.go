package zhipu

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

// CatalogEntry is one model from GET {chatBase}/models. Zhipu's OpenAI-
// compatible list uses the standard data[].id shape; owned_by and object are
// ignored.
type CatalogEntry struct {
	ID            string `json:"id"`
	Object        string `json:"object"`
	OwnedBy       string `json:"owned_by"`
	ContextWindow int    `json:"context_window"`
}

type catalogEnvelope struct {
	Data []CatalogEntry `json:"data"`
}

const catalogTTL = 30 * time.Minute

// ParseCatalogJSON maps the upstream model list onto ModelInfo. Public and
// native ids stay equal so a client can request the id Zhipu published.
func ParseCatalogJSON(raw []byte) ([]providers.ModelInfo, error) {
	var env catalogEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("zhipu catalog: %w", err)
	}
	if len(env.Data) == 0 {
		return nil, fmt.Errorf("zhipu catalog empty")
	}
	models := make([]providers.ModelInfo, 0, len(env.Data))
	seen := map[string]struct{}{}
	for _, entry := range env.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[strings.ToLower(id)]; dup {
			continue
		}
		seen[strings.ToLower(id)] = struct{}{}
		caps := reasoningCaps(id)
		if entry.ContextWindow > 0 {
			caps.ContextWindow = entry.ContextWindow
		}
		models = append(models, providers.ModelInfo{
			NativeModel:  id,
			PublicModel:  id,
			DisplayName:  id,
			Capabilities: caps,
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("zhipu catalog produced no models")
	}
	return models, nil
}

func (c *Client) fetchCatalog(ctx context.Context, accountID string, credential Credential) ([]providers.ModelInfo, error) {
	base, err := credential.ChatBase()
	if err != nil {
		return nil, err
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, chatEndpoint(base, "/models"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.APIKey)
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, newProviderError(resp.StatusCode, string(body))
	}
	return ParseCatalogJSON(body)
}

// Models implements providers.ModelCatalogProvider. The list is per account
// because pay-as-you-go and Coding Plan keys do not see the same models.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	if models, ok := c.cachedModels(accountID, time.Now()); ok {
		return models, nil
	}
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	models, err := c.fetchCatalog(ctx, accountID, credential)
	if err != nil {
		return nil, err
	}
	c.rememberModels(accountID, models, time.Now())
	return cloneModels(models), nil
}

func (c *Client) cachedModels(accountID string, now time.Time) ([]providers.ModelInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.catalogs[accountID]
	if !ok || len(entry.models) == 0 || now.Sub(entry.at) > catalogTTL {
		return nil, false
	}
	return cloneModels(entry.models), true
}

func (c *Client) rememberModels(accountID string, models []providers.ModelInfo, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.catalogs == nil {
		c.catalogs = map[string]catalogCache{}
	}
	c.catalogs[accountID] = catalogCache{models: cloneModels(models), at: at}
}

func (c *Client) lookupModel(accountID, model string) (providers.ModelInfo, bool) {
	want := strings.ToLower(strings.TrimSpace(model))
	if want == "" {
		return providers.ModelInfo{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.catalogs[accountID]
	if !ok {
		return providers.ModelInfo{}, false
	}
	for _, info := range entry.models {
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
	for i, model := range in {
		out[i] = model
		if len(model.Capabilities.ReasoningOptions) > 0 {
			out[i].Capabilities.ReasoningOptions = append([]string(nil), model.Capabilities.ReasoningOptions...)
		}
	}
	return out
}

// chatEndpoint appends an OpenAI path without doubling a version segment.
// A base ending in /v4 or /v1 gets /chat/completions; a bare origin gets
// /v1/chat/completions.
func chatEndpoint(base, endpoint string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	endpoint = "/" + strings.TrimLeft(strings.TrimSpace(endpoint), "/")
	relative := strings.TrimPrefix(endpoint, "/v1")
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, endpoint) || strings.HasSuffix(lower, relative) {
		return base
	}
	if hasVersionSuffix(lower) {
		return base + relative
	}
	return base + endpoint
}

func hasVersionSuffix(base string) bool {
	slash := strings.LastIndex(base, "/")
	if slash < 0 || slash == len(base)-1 {
		return false
	}
	segment := base[slash+1:]
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, r := range segment[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
