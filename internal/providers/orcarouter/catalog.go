package orcarouter

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

// Bounds on the live catalog response so a hostile or broken upstream cannot
// consume unbounded memory or advertise an unbounded number of routes.
const (
	catalogTimeout     = 15 * time.Second
	maxCatalogBytes    = 8 << 20
	maxCatalogItems    = 5000
	maxCatalogIDRunes  = 200
	catalogCacheTTL    = 5 * time.Minute
	contextLengthLimit = 100 << 20
)

// supportedEndpointTypes are the endpoint types this adapter recognises and can
// route to through the inference API. A record advertising none of them is
// dropped: it would be a route this client cannot speak.
var supportedEndpointTypes = map[string]struct{}{
	"openai":           {},
	"anthropic":        {},
	"gemini":           {},
	"openai-response":  {},
	"embedding":        {},
	"embeddings":       {},
	"image-generation": {},
	"image_generation": {},
	"openai-video":     {},
	"jina-rerank":      {},
	"rerank":           {},
}

// textEndpointTypes is the subset a text chat turn can be sent over. A record
// whose only endpoint types are embeddings / image-generation / video / rerank
// is not a chat model even though the gateway lists it.
var textEndpointTypes = map[string]struct{}{
	"openai":          {},
	"anthropic":       {},
	"gemini":          {},
	"openai-response": {},
}

// nonTextEndpointTypes are endpoint types whose presence alone means the record
// is not a general chat model.
var nonTextEndpointTypes = map[string]struct{}{
	"embedding":        {},
	"embeddings":       {},
	"image-generation": {},
	"image_generation": {},
	"openai-video":     {},
	"jina-rerank":      {},
	"rerank":           {},
}

// capabilityChat / Embedding / Image / Video / Rerank are the filter names the
// catalog query and the selector use.
const (
	CapabilityChat      = "chat"
	CapabilityEmbedding = "embedding"
	CapabilityImage     = "image"
	CapabilityVideo     = "video"
	CapabilityRerank    = "rerank"
	CapabilityVision    = "vision"
	CapabilityAudio     = "audio"
)

type catalogEnvelope struct {
	Data []catalogEntry `json:"data"`
}

type catalogEntry struct {
	ID                     string   `json:"id"`
	Object                 string   `json:"object"`
	Name                   string   `json:"name"`
	OwnedBy                string   `json:"owned_by"`
	ContextLength          int      `json:"context_length"`
	MaxCompletionTokens    int      `json:"max_completion_tokens"`
	SupportedEndpointTypes []string `json:"supported_endpoint_types"`
	Architecture           struct {
		InputModalities  []string `json:"input_modalities"`
		OutputModalities []string `json:"output_modalities"`
	} `json:"architecture"`
	TopProvider struct {
		ContextLength       int `json:"context_length"`
		MaxCompletionTokens int `json:"max_completion_tokens"`
	} `json:"top_provider"`
}

// ParseCatalogJSON maps a GET /v1/models payload onto the provider ModelInfo
// surface. Model ids keep their "<vendor>/<model>" namespace verbatim. Records
// with no id, a duplicate id, an unknown endpoint type, or no endpoint type the
// client can speak are dropped; capabilities are never inferred from the name.
func ParseCatalogJSON(raw []byte) ([]providers.ModelInfo, error) {
	var env catalogEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.Data) > maxCatalogItems {
		return nil, fmt.Errorf("orcarouter catalog exceeds %d items", maxCatalogItems)
	}
	models := make([]providers.ModelInfo, 0, len(env.Data))
	seen := map[string]struct{}{}
	for _, entry := range env.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" || len([]rune(id)) > maxCatalogIDRunes {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		endpoints := normalizeList(entry.SupportedEndpointTypes)
		if !hasSpeakableEndpoint(endpoints) {
			continue
		}
		seen[id] = struct{}{}
		context := entry.ContextLength
		if entry.TopProvider.ContextLength > context {
			context = entry.TopProvider.ContextLength
		}
		output := entry.MaxCompletionTokens
		if entry.TopProvider.MaxCompletionTokens > output {
			output = entry.TopProvider.MaxCompletionTokens
		}
		models = append(models, providers.ModelInfo{
			NativeModel:  id,
			PublicModel:  id,
			DisplayName:  firstNonEmpty(entry.Name, id),
			Capabilities: capabilitiesFrom(entry, endpoints, context, output),
		})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("orcarouter catalog produced no usable models")
	}
	return models, nil
}

func capabilitiesFrom(entry catalogEntry, endpoints []string, context, output int) providers.ModelCapabilities {
	inputs := normalizeList(entry.Architecture.InputModalities)
	outputs := normalizeList(entry.Architecture.OutputModalities)
	caps := providers.ModelCapabilities{
		ContextWindow:    clampContext(context),
		MaxOutput:        clampContext(output),
		Tools:            true,
		EndpointTypes:    endpoints,
		InputModalities:  inputs,
		OutputModalities: outputs,
	}
	caps.ImageInput = hasModality(inputs, "image")
	caps.AudioInput = hasModality(inputs, "audio")
	caps.VideoInput = hasModality(inputs, "video")
	caps.Images = caps.ImageInput
	caps.Embedding = matchEndpoint(endpoints, "embedding", "embeddings")
	caps.ImageGeneration = matchEndpoint(endpoints, "image-generation", "image_generation")
	caps.Rerank = matchEndpoint(endpoints, "jina-rerank", "rerank")
	return caps
}

// hasSpeakableEndpoint reports whether the adapter recognises any of this
// record's endpoint types. A record whose every endpoint type is unknown is a
// route this client cannot speak and may never be advertised.
func hasSpeakableEndpoint(endpoints []string) bool {
	for _, endpoint := range endpoints {
		if _, ok := supportedEndpointTypes[endpoint]; ok {
			return true
		}
	}
	return false
}

func matchEndpoint(endpoints []string, wants ...string) bool {
	for _, endpoint := range endpoints {
		for _, want := range wants {
			if endpoint == want {
				return true
			}
		}
	}
	return false
}

func hasModality(modalities []string, want string) bool {
	for _, modality := range modalities {
		if modality == want {
			return true
		}
	}
	return false
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		item := strings.ToLower(strings.TrimSpace(value))
		if item == "" {
			continue
		}
		if _, dup := seen[item]; dup {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func clampContext(value int) int {
	if value < 0 || value > contextLengthLimit {
		return 0
	}
	return value
}

// MatchesCapability reports whether a model may appear in the selector for one
// AI entry point. It is the single filter every capability-scoped dropdown goes
// through, so "no incompatible attachment" and "filtered model list" cannot
// drift apart.
//
// Text chat and multimodal both require an endpoint type the client can speak
// (openai / anthropic / gemini / openai-response) and a record that is not a
// non-text-only route. Multimodal additionally requires the catalog to declare
// the modality in architecture.input_modalities — an undeclared modality fails
// closed rather than being guessed from the model name.
func MatchesCapability(model providers.ModelInfo, capability string) bool {
	endpoints := model.Capabilities.EndpointTypes
	switch capability {
	case CapabilityChat, "":
		return isTextChat(endpoints)
	case CapabilityVision:
		return isTextChat(endpoints) && model.Capabilities.ImageInput
	case CapabilityAudio:
		return isTextChat(endpoints) && model.Capabilities.AudioInput
	case CapabilityVideo:
		return isTextChat(endpoints) && model.Capabilities.VideoInput
	case CapabilityEmbedding:
		return matchEndpoint(endpoints, "embedding", "embeddings")
	case CapabilityImage:
		return matchEndpoint(endpoints, "image-generation", "image_generation")
	case CapabilityVideoGeneration:
		return matchEndpoint(endpoints, "openai-video")
	case CapabilityRerank:
		return matchEndpoint(endpoints, "jina-rerank", "rerank")
	default:
		return false
	}
}

// CapabilityVideoGeneration is the separate video-generation capability; it is
// distinct from CapabilityVideo, which asks for video *input* on a chat model.
const CapabilityVideoGeneration = "video-generation"

func isTextChat(endpoints []string) bool {
	if len(endpoints) == 0 {
		return false
	}
	speakable := false
	for _, endpoint := range endpoints {
		if _, ok := textEndpointTypes[endpoint]; ok {
			speakable = true
			continue
		}
		if _, ok := nonTextEndpointTypes[endpoint]; ok {
			return false
		}
	}
	return speakable
}

// FilterModels returns the models a given entry point may offer. Order is the
// catalog order so the selector is stable between refreshes.
func FilterModels(models []providers.ModelInfo, capability string) []providers.ModelInfo {
	out := make([]providers.ModelInfo, 0, len(models))
	for _, model := range models {
		if MatchesCapability(model, capability) {
			out = append(out, model)
		}
	}
	return out
}

// fetchCatalog retrieves and parses the live model list for one credential.
func (c *Client) fetchCatalog(ctx context.Context, credential Credential, capability string) ([]providers.ModelInfo, error) {
	client, err := c.httpClient(ctx, "")
	if err != nil {
		return nil, err
	}
	scoped := *client
	scoped.Timeout = catalogTimeout
	endpoint := c.apiBase(ctx, credential) + ModelsPath
	if strings.TrimSpace(capability) != "" {
		endpoint += "?capability=" + capability
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+credential.APIKey)
	req.Header.Set("Accept", "application/json")
	for name, value := range credential.Headers {
		req.Header.Set(name, value)
	}
	resp, err := scoped.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, newProviderError(resp.StatusCode, string(body))
	}
	return ParseCatalogJSON(body)
}

// Models implements providers.ModelCatalogProvider. A warm cache short-circuits
// the network; otherwise the live catalog is authoritative. If live discovery
// fails and nothing is cached, the verified seed is returned so a fresh install
// still has a usable selector.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	if models, ok := c.cachedModels(CapabilityChat, time.Now()); ok {
		return cloneModels(models), nil
	}
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return seedModels(), nil
	}
	models, err := c.fetchCatalog(ctx, credential, CapabilityChat)
	if err != nil {
		if models, ok := c.cachedModels(CapabilityChat, time.Now()); ok {
			return cloneModels(models), nil
		}
		return seedModels(), nil
	}
	// The catalog request asks the gateway for chat models; the local filter is
	// the authoritative guard. A record only stays in the text-chat selector when
	// it declares an endpoint type this client can speak and declares no
	// non-text-only endpoint (image-generation / openai-video / jina-rerank), so
	// an upstream that ignores the query hint can never leak a non-chat model in.
	models = FilterModels(models, CapabilityChat)
	if len(models) == 0 {
		if cached, ok := c.cachedModels(CapabilityChat, time.Now()); ok {
			return cloneModels(cached), nil
		}
		return seedModels(), nil
	}
	c.rememberModels(CapabilityChat, models, time.Now())
	return cloneModels(models), nil
}

func (c *Client) cachedModels(capability string, now time.Time) ([]providers.ModelInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.catalog[capability]
	if !ok || len(entry.models) == 0 || entry.at.IsZero() || now.Sub(entry.at) > catalogCacheTTL {
		return nil, false
	}
	return entry.models, true
}

func (c *Client) rememberModels(capability string, models []providers.ModelInfo, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.catalog == nil {
		c.catalog = map[string]catalogCacheEntry{}
	}
	c.catalog[capability] = catalogCacheEntry{models: cloneModels(models), at: at}
}

func (c *Client) lookupModel(model string) (providers.ModelInfo, bool) {
	want := strings.ToLower(strings.TrimSpace(model))
	if want == "" {
		return providers.ModelInfo{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, entry := range c.catalog {
		for _, info := range entry.models {
			if strings.ToLower(info.NativeModel) == want || strings.ToLower(info.PublicModel) == want {
				return info, true
			}
		}
	}
	for _, info := range seedModels() {
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
		out[i].Capabilities.ReasoningOptions = append([]string(nil), m.Capabilities.ReasoningOptions...)
		out[i].Capabilities.InputModalities = append([]string(nil), m.Capabilities.InputModalities...)
		out[i].Capabilities.OutputModalities = append([]string(nil), m.Capabilities.OutputModalities...)
		out[i].Capabilities.EndpointTypes = append([]string(nil), m.Capabilities.EndpointTypes...)
	}
	return out
}
