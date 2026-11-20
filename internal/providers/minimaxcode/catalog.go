package minimaxcode

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

func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	c.mu.Lock()
	cached, ok := c.catalogs[accountID]
	c.mu.Unlock()
	if ok && c.now().Sub(cached.at) < catalogTTL && len(cached.models) > 0 {
		return cached.models, nil
	}
	models, err := c.fetchCatalogModels(ctx, accountID)
	if err != nil || len(models) == 0 {
		models = staticModels()
		err = nil
	}
	c.mu.Lock()
	c.catalogs[accountID] = catalogCache{models: models, at: c.now()}
	c.mu.Unlock()
	return models, err
}

func (c *Client) fetchCatalogModels(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	endpoint := credential.agentBase() + modelsPath + "?region=" + urlQueryEscape(credential.region()) + "&buildEnv=prod"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.AccessToken)
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("X-Mavis-Agent-Id", AgentID)
	req.Header.Set("Accept", "application/json")
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("minimaxcode model catalog failed (HTTP %d): %s", resp.StatusCode, truncate(string(body), 200))
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return parseModels(raw)
}

func parseModels(raw []byte) ([]providers.ModelInfo, error) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, fmt.Errorf("minimaxcode model catalog is not valid json")
	}
	var entries []any
	if providersList, ok := payload["providers"].([]any); ok {
		for _, rawProvider := range providersList {
			provider, _ := rawProvider.(map[string]any)
			config, _ := provider["config"].(map[string]any)
			if models, ok := config["models"].([]any); ok {
				entries = append(entries, models...)
			}
		}
	}
	if len(entries) == 0 {
		if models, ok := payload["models"].([]any); ok {
			entries = models
		}
	}
	if len(entries) == 0 {
		if data, ok := payload["data"].([]any); ok {
			entries = data
		}
	}
	models := make([]providers.ModelInfo, 0, len(entries))
	for _, rawModel := range entries {
		model, ok := rawModel.(map[string]any)
		if !ok {
			continue
		}
		id := first(jsonStr(model, "model"), jsonStr(model, "id"), jsonStr(model, "model_name"), jsonStr(model, "name"))
		if id == "" {
			continue
		}
		name := first(jsonStr(model, "display_name"), jsonStr(model, "name"), id)
		models = append(models, providers.ModelInfo{NativeModel: id, PublicModel: id, DisplayName: name, Capabilities: providers.ModelCapabilities{Tools: true}})
	}
	return models, nil
}

func staticModels() []providers.ModelInfo {
	models := make([]providers.ModelInfo, 0, len(knownModels))
	for _, id := range knownModels {
		models = append(models, providers.ModelInfo{NativeModel: id, PublicModel: id, DisplayName: id, Capabilities: providers.ModelCapabilities{Tools: true}})
	}
	return models
}

func normalizedModelKey(snapshot []providers.ModelInfo, requested string) (string, bool) {
	want := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(requested), ProviderID+"/"))
	if want == "" {
		return "", false
	}
	for _, info := range snapshot {
		if strings.ToLower(info.NativeModel) == want || strings.ToLower(info.PublicModel) == want {
			return info.NativeModel, true
		}
	}
	return "", false
}

func jsonStr(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return strings.TrimSpace(value)
}

func urlQueryEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, " ", "%20"), "/", "%2F")
}

var _ = time.Second
