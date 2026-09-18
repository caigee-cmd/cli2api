package devin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// CatalogEntry is one model from the Devin models fixture / remote JSON.
type CatalogEntry struct {
	ID                       string   `json:"id"`
	Type                     string   `json:"type"`
	DisplayName              string   `json:"display_name"`
	ContextLength            int      `json:"context_length"`
	MaxCompletionTokens      int      `json:"max_completion_tokens"`
	SupportedInputModalities []string `json:"supportedInputModalities"`
	Thinking                 *struct {
		Levels []string `json:"levels"`
	} `json:"thinking"`
}

type catalogFile struct {
	Devin []CatalogEntry `json:"devin"`
}

const catalogTTL = 3 * time.Hour

var catalogURLs = []string{
	"https://raw.githubusercontent.com/router-for-me/models/refs/heads/main/devin_models.json",
	"https://models.router-for.me/devin_models.json",
}

var (
	catalogMu sync.RWMutex

	cachedCatalog []providers.ModelInfo
	cachedLevels  map[string][]string
	cachedAt      time.Time

	catalogHTTPClient = &http.Client{Timeout: 20 * time.Second}
)

func ClearCatalog() {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	cachedCatalog = nil
	cachedLevels = nil
	cachedAt = time.Time{}
}

func currentLevels() map[string][]string {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	return cloneLevels(cachedLevels)
}

func cloneLevels(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func cloneModels(in []providers.ModelInfo) []providers.ModelInfo {
	if len(in) == 0 {
		return nil
	}
	return append([]providers.ModelInfo(nil), in...)
}

// LoadCatalogFixture parses the sample/remote JSON shape used by Devin models.
func LoadCatalogFixture(path string) ([]providers.ModelInfo, map[string][]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return ParseCatalogJSON(raw)
}

func ParseCatalogJSON(raw []byte) ([]providers.ModelInfo, map[string][]string, error) {
	var file catalogFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, nil, err
	}
	if len(file.Devin) == 0 {
		return nil, nil, fmt.Errorf("devin catalog empty")
	}
	models := make([]providers.ModelInfo, 0, len(file.Devin))
	levels := make(map[string][]string, len(file.Devin))
	for _, entry := range file.Devin {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		caps := providers.ModelCapabilities{
			ContextWindow: entry.ContextLength,
			MaxOutput:     entry.MaxCompletionTokens,
		}
		for _, m := range entry.SupportedInputModalities {
			switch strings.ToLower(m) {
			case "image":
				caps.Images = true
			case "text":
				// always text
			}
		}
		if entry.Thinking != nil && len(entry.Thinking.Levels) > 0 {
			caps.Reasoning = true
			caps.ReasoningOptions = append([]string(nil), entry.Thinking.Levels...)
			levels[id] = append([]string(nil), entry.Thinking.Levels...)
		}
		caps.Tools = true
		models = append(models, providers.ModelInfo{
			NativeModel:  id,
			PublicModel:  PublicModelID(id),
			DisplayName:  firstNonEmpty(entry.DisplayName, id),
			Capabilities: caps,
		})
	}
	if len(models) == 0 {
		return nil, nil, fmt.Errorf("devin catalog produced no models")
	}
	return models, levels, nil
}

func setCachedCatalog(models []providers.ModelInfo, levels map[string][]string, at time.Time) {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	cachedCatalog = cloneModels(models)
	cachedLevels = cloneLevels(levels)
	cachedAt = at
}

func cachedCatalogSnapshot(now time.Time) ([]providers.ModelInfo, map[string][]string, bool) {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	if len(cachedCatalog) == 0 || cachedAt.IsZero() || now.Sub(cachedAt) > catalogTTL {
		return nil, nil, false
	}
	return cloneModels(cachedCatalog), cloneLevels(cachedLevels), true
}

func fetchRemoteCatalogJSON(ctx context.Context, client *http.Client, urls []string) ([]byte, string, error) {
	if client == nil {
		client = catalogHTTPClient
	}
	var errs []string
	for _, rawURL := range urls {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", rawURL, err))
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", rawURL, readErr))
			continue
		}
		if resp.StatusCode >= 300 {
			errs = append(errs, fmt.Sprintf("%s: status %d", rawURL, resp.StatusCode))
			continue
		}
		if len(bytesTrimSpace(body)) == 0 {
			errs = append(errs, fmt.Sprintf("%s: empty body", rawURL))
			continue
		}
		return body, rawURL, nil
	}
	if len(errs) == 0 {
		return nil, "", fmt.Errorf("devin catalog unavailable: no remote sources")
	}
	return nil, "", fmt.Errorf("devin catalog unavailable: %s", strings.Join(errs, "; "))
}

func bytesTrimSpace(raw []byte) []byte {
	return []byte(strings.TrimSpace(string(raw)))
}

// FetchRemoteCatalog pulls the public Devin models JSON, parses it, and updates
// the in-memory TTL cache. It never fabricates a static production catalog.
func FetchRemoteCatalog(ctx context.Context) ([]providers.ModelInfo, map[string][]string, error) {
	raw, source, err := fetchRemoteCatalogJSON(ctx, catalogHTTPClient, catalogURLs)
	if err != nil {
		return nil, nil, err
	}
	models, levels, err := ParseCatalogJSON(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("devin catalog parse failed from %s: %w", source, err)
	}
	setCachedCatalog(models, levels, time.Now())
	return cloneModels(models), cloneLevels(levels), nil
}

// FetchCliModelConfigs refreshes the TTL-cached remote JSON catalog. The Connect
// unary GetCliModelConfigs protobuf path remains a future optimization; this
// stage intentionally uses the same public JSON sources as upstream.
func FetchCliModelConfigs(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	_ = accountID
	if models, _, ok := cachedCatalogSnapshot(time.Now()); ok {
		return models, nil
	}
	models, _, err := FetchRemoteCatalog(ctx)
	if err != nil {
		return nil, err
	}
	return models, nil
}

func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	if _, err := c.credential(ctx, accountID); err != nil {
		return nil, err
	}
	if models, _, ok := cachedCatalogSnapshot(time.Now()); ok {
		return models, nil
	}
	models, err := FetchCliModelConfigs(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("devin catalog unavailable: empty after fetch")
	}
	return models, nil
}
