package kimi

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

// catalogRecord is one row of the managed GET /models list. Both the current
// nested shape (limit/reasoning/modalities/tools) and the legacy flat fields
// older servers still emit are decoded, because a token can outlive a server
// rollout in either direction.
type catalogRecord struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Limit       *struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Reasoning *struct {
		Type   string `json:"type"`
		Effort *struct {
			Valid   []string `json:"valid"`
			Default string   `json:"default"`
		} `json:"effort"`
	} `json:"reasoning"`
	Modalities *struct {
		Input  []string `json:"input"`
		Output []string `json:"output"`
	} `json:"modalities"`
	SupportsToolUse *bool `json:"supports_tool_use"`
	Tools           *struct {
		DynamicLoad bool `json:"dynamic_load"`
	} `json:"tools"`
	Protocol string `json:"protocol"`

	// Legacy flat declarations.
	ContextLength        int      `json:"context_length"`
	SupportsReasoning    bool     `json:"supports_reasoning"`
	SupportsImageIn      bool     `json:"supports_image_in"`
	SupportsVideoIn      bool     `json:"supports_video_in"`
	SupportsThinkingType string   `json:"supports_thinking_type"`
	SupportEfforts       []string `json:"support_efforts"`
	DefaultEffort        string   `json:"default_effort"`
	ThinkEfforts         *struct {
		Support       bool     `json:"support"`
		ValidEfforts  []string `json:"valid_efforts"`
		DefaultEffort string   `json:"default_effort"`
	} `json:"think_efforts"`
	SupportsDynamicTools bool `json:"supports_dynamic_tools"`
}

type catalogEnvelope struct {
	Data []catalogRecord `json:"data"`
}

// ParseCatalogJSON maps the managed model list onto ModelInfo. Public and
// native ids stay equal: the managed endpoint already publishes the id the
// chat endpoint accepts.
func ParseCatalogJSON(raw []byte) ([]providers.ModelInfo, error) {
	var envelope catalogEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("kimi catalog: %w", err)
	}
	if len(envelope.Data) == 0 {
		return nil, fmt.Errorf("kimi catalog is empty")
	}
	models := make([]providers.ModelInfo, 0, len(envelope.Data))
	seen := map[string]struct{}{}
	for _, record := range envelope.Data {
		id := strings.TrimSpace(record.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[strings.ToLower(id)]; dup {
			continue
		}
		seen[strings.ToLower(id)] = struct{}{}
		models = append(models, toModelInfo(record))
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("kimi catalog produced no models")
	}
	return models, nil
}

// toModelInfo maps one record. Capabilities come from the server declaration
// only; the adapter never invents thinking, vision or tool support.
func toModelInfo(record catalogRecord) providers.ModelInfo {
	id := strings.TrimSpace(record.ID)
	contextWindow := 0
	maxOutput := 0
	if record.Limit != nil {
		contextWindow = record.Limit.Context
		maxOutput = record.Limit.Output
	}
	if contextWindow <= 0 {
		contextWindow = record.ContextLength
	}

	caps := providers.ModelCapabilities{
		ContextWindow:    contextWindow,
		ContextWindowMax: contextWindow,
		MaxOutput:        maxOutput,
		Tools:            supportsTools(record),
		Images:           supportsImages(record),
	}
	thinkingType := record.SupportsThinkingType
	efforts := record.SupportEfforts
	defaultEffort := record.DefaultEffort
	if record.ThinkEfforts != nil && record.ThinkEfforts.Support {
		efforts = record.ThinkEfforts.ValidEfforts
		if record.ThinkEfforts.DefaultEffort != "" {
			defaultEffort = record.ThinkEfforts.DefaultEffort
		}
	}
	if record.Reasoning != nil {
		switch record.Reasoning.Type {
		case "only", "both", "none":
			thinkingType = record.Reasoning.Type
		}
		if record.Reasoning.Effort != nil {
			efforts = record.Reasoning.Effort.Valid
			if record.Reasoning.Effort.Default != "" {
				defaultEffort = record.Reasoning.Effort.Default
			}
		}
	}
	switch thinkingType {
	case "only":
		caps.Reasoning = true
		caps.CanDisableThinking = false
	case "both":
		caps.Reasoning = true
		caps.CanDisableThinking = true
	case "none":
		caps.Reasoning = false
	case "":
		caps.Reasoning = record.SupportsReasoning
	}

	if options := providers.UniqueReasoningOptions(efforts); len(options) > 0 {
		caps.ReasoningOptions = options
		caps.ReasoningType = "effort"
		caps.ReasoningDefault = resolveDefaultEffort(defaultEffort, options)
		caps.Reasoning = true
		if thinkingType != "both" {
			caps.CanDisableThinking = false
		}
	}

	displayName := strings.TrimSpace(record.DisplayName)
	return providers.ModelInfo{
		NativeModel:  id,
		PublicModel:  id,
		DisplayName:  displayName,
		Capabilities: caps,
		Scene:        strings.TrimSpace(record.Protocol),
	}
}

// supportsTools reads the declared tool capability. The managed catalog omits
// `supports_tool_use` on every model it currently returns — verified against a
// live CN account whose models then completed real tool calls — and Kimi Code's
// own client treats an absent field as supported, so absent means true. A model
// that cannot call tools is expected to declare `supports_tool_use: false`.
func supportsTools(record catalogRecord) bool {
	if record.SupportsToolUse != nil {
		return *record.SupportsToolUse
	}
	return true
}

func supportsImages(record catalogRecord) bool {
	if record.Modalities != nil {
		for _, modality := range record.Modalities.Input {
			if strings.EqualFold(strings.TrimSpace(modality), "image") {
				return true
			}
		}
	}
	return record.SupportsImageIn
}

func resolveDefaultEffort(raw string, options []string) string {
	normalized := providers.NormalizeReasoningLevel(raw)
	if normalized == "" {
		return ""
	}
	for _, option := range options {
		if option == normalized {
			return normalized
		}
	}
	return ""
}

// fetchCatalogModels reads GET /models with a usable token.
func (c *Client) fetchCatalogModels(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	credential, token, err := c.accessToken(ctx, accountID)
	if err != nil {
		return nil, err
	}
	base := c.apiBase(credential)
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, base+modelsPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	applyIdentityHeaders(req, credential.DeviceIDOr())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
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
// because the managed endpoint publishes what that subscription can serve.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	if models, ok := c.cachedModels(accountID, c.now()); ok {
		return models, nil
	}
	models, err := c.fetchCatalogModels(ctx, accountID)
	if err != nil {
		return nil, err
	}
	c.rememberModels(accountID, models, c.now())
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

// lookupModel reads the cached catalog for one model id.
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
