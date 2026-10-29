package codearts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// modelRecord is one upstream model declaration. ContextWindow and MaxTokens
// are the only capability numbers the catalog publishes.
type modelRecord struct {
	ID            string
	Name          string
	Description   string
	ContextWindow int64
	MaxTokens     int64
	Benefit       bool
}

// fetchModels merges the three official sources for one account. A source that
// fails is skipped; only an empty merge is an error.
func (c *Client) fetchModels(ctx context.Context, accountID string) ([]providers.ModelInfo, map[string]bool, error) {
	credential, err := c.credential(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	merged := map[string]modelRecord{}
	benefit := map[string]bool{}
	if records, fetchErr := c.fetchAgentModels(ctx, accountID, credential); fetchErr == nil {
		mergeRecords(merged, records)
	}
	if records, fetchErr := c.fetchBuiltinModels(ctx, accountID, credential); fetchErr == nil {
		mergeRecords(merged, records)
	}
	if records, fetchErr := c.fetchBenefitModels(ctx, accountID, credential); fetchErr == nil {
		for _, record := range records {
			record.Benefit = true
			benefit[strings.ToLower(record.ID)] = true
			mergeRecords(merged, []modelRecord{record})
		}
	}
	if len(merged) == 0 {
		return nil, nil, fmt.Errorf("codearts model discovery returned no models")
	}
	records := make([]modelRecord, 0, len(merged))
	for _, record := range merged {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		left, right := strings.ToLower(records[i].ID), strings.ToLower(records[j].ID)
		if left != right {
			return left < right
		}
		return records[i].ID < records[j].ID
	})
	models := make([]providers.ModelInfo, 0, len(records))
	for _, record := range records {
		models = append(models, toModelInfo(record))
	}
	return models, benefit, nil
}

func mergeRecords(target map[string]modelRecord, records []modelRecord) {
	for _, record := range records {
		id := strings.TrimSpace(record.ID)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if current, ok := target[key]; ok {
			if current.ContextWindow == 0 {
				current.ContextWindow = record.ContextWindow
			}
			if current.MaxTokens == 0 {
				current.MaxTokens = record.MaxTokens
			}
			if current.Name == "" {
				current.Name = record.Name
			}
			if current.Description == "" {
				current.Description = record.Description
			}
			current.Benefit = current.Benefit || record.Benefit
			target[key] = current
			continue
		}
		target[key] = record
	}
}

// toModelInfo declares only what the upstream catalog publishes. Tools are a
// provider-level capability (the chat endpoint accepts them natively); images
// are not declared because the request mapper is text-only.
func toModelInfo(record modelRecord) providers.ModelInfo {
	maxOutput := record.MaxTokens
	if maxOutput > 65536 {
		// The upstream rejects max_tokens above 65536 with
		// InferHub.001001005 even when the catalog advertises more.
		maxOutput = 65536
	}
	caps := providers.ModelCapabilities{
		ContextWindow:    int(record.ContextWindow),
		ContextWindowMax: int(record.ContextWindow),
		MaxOutput:        int(maxOutput),
		Tools:            true,
		Reasoning:        true,
	}
	return providers.ModelInfo{
		NativeModel:  record.ID,
		PublicModel:  record.ID,
		DisplayName:  firstNonEmpty(record.Name, record.ID),
		Capabilities: caps,
	}
}

// Models implements providers.ModelCatalogProvider.
func (c *Client) Models(ctx context.Context, accountID string) ([]providers.ModelInfo, error) {
	if models, _, ok := c.cachedCatalog(accountID); ok {
		return models, nil
	}
	models, benefit, err := c.fetchModels(ctx, accountID)
	if err != nil {
		return nil, err
	}
	c.rememberCatalog(accountID, models, benefit)
	return cloneModels(models), nil
}

func (c *Client) cachedCatalog(accountID string) ([]providers.ModelInfo, map[string]bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.catalogs[accountID]
	if !ok || len(entry.models) == 0 || c.now().Sub(entry.at) > catalogTTL {
		return nil, nil, false
	}
	return cloneModels(entry.models), cloneBoolMap(entry.benefit), true
}

func (c *Client) rememberCatalog(accountID string, models []providers.ModelInfo, benefit map[string]bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.catalogs == nil {
		c.catalogs = map[string]catalogCache{}
	}
	c.catalogs[accountID] = catalogCache{models: cloneModels(models), benefit: cloneBoolMap(benefit), at: c.now()}
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

func cloneBoolMap(in map[string]bool) map[string]bool {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// canonicalModel returns the exact catalog spelling for a client-supplied id.
func (c *Client) canonicalModel(accountID, model string) string {
	want := strings.ToLower(strings.TrimSpace(model))
	if want == "" {
		return ""
	}
	models, _, ok := c.cachedCatalog(accountID)
	if !ok {
		return model
	}
	for _, info := range models {
		if strings.ToLower(info.NativeModel) == want || strings.ToLower(info.PublicModel) == want {
			return info.NativeModel
		}
	}
	return model
}

// isBenefitModel reports whether the account's catalog routes this model
// through the limited-benefit gateway.
func (c *Client) isBenefitModel(accountID, model string) bool {
	_, benefit, ok := c.cachedCatalog(accountID)
	if !ok {
		return false
	}
	return benefit[strings.ToLower(strings.TrimSpace(model))]
}

// fetchAgentModels reads the CodeAgent detail's gpts.models.
func (c *Client) fetchAgentModels(ctx context.Context, accountID string, credential Credential) ([]modelRecord, error) {
	agentID, err := c.defaultAgentID(ctx, accountID, credential)
	if err != nil || agentID == "" {
		return nil, err
	}
	query := url.Values{}
	query.Set("agent_id", agentID)
	raw, err := c.signedGet(ctx, accountID, c.apiBase(credential)+agentDetailPath+"?"+query.Encode(), credential, true)
	if err != nil {
		return nil, err
	}
	var detail struct {
		Gpts struct {
			Models []struct {
				ModelAlias string `json:"model_alias"`
				ModelName  string `json:"model_name"`
				ModelID    string `json:"model_id"`
				Params     struct {
					ContextWindow int64  `json:"context_window"`
					MaxTokens     int64  `json:"max_tokens"`
					ModelID       string `json:"model_id"`
					ModelDesc     string `json:"model_desc"`
				} `json:"model_parameters"`
			} `json:"models"`
		} `json:"gpts"`
	}
	if err := json.Unmarshal(raw, &detail); err != nil {
		return nil, fmt.Errorf("parse codearts agent models: %w", err)
	}
	out := make([]modelRecord, 0, len(detail.Gpts.Models))
	for _, model := range detail.Gpts.Models {
		id := firstNonEmpty(model.ModelName, model.ModelID, model.ModelAlias, model.Params.ModelID)
		if id == "" {
			continue
		}
		out = append(out, modelRecord{
			ID: id, Name: id, Description: model.Params.ModelDesc,
			ContextWindow: model.Params.ContextWindow, MaxTokens: model.Params.MaxTokens,
		})
	}
	return out, nil
}

func (c *Client) defaultAgentID(ctx context.Context, accountID string, credential Credential) (string, error) {
	raw, err := c.signedGet(ctx, accountID, c.apiBase(credential)+agentListPath+"?offset=0&limit=100&is_primary_agent=true", credential, true)
	if err != nil {
		return "", err
	}
	var out struct {
		Agents []struct {
			AgentID   string `json:"agent_id"`
			AgentName string `json:"agent_name"`
			Primary   bool   `json:"is_primary_agent"`
			ShowInIDE bool   `json:"show_in_ide"`
			Alias     struct {
				ZhCN string `json:"alias_zh_cn"`
			} `json:"alias"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("parse codearts agents: %w", err)
	}
	for _, agent := range out.Agents {
		if agent.AgentName == "CodeAgent" && agent.Alias.ZhCN == "智能体" && agent.ShowInIDE {
			return agent.AgentID, nil
		}
	}
	for _, agent := range out.Agents {
		if agent.Primary {
			return agent.AgentID, nil
		}
	}
	if len(out.Agents) > 0 {
		return out.Agents[0].AgentID, nil
	}
	return "", nil
}

// fetchBuiltinModels reads GET /v1/model/builtin.
func (c *Client) fetchBuiltinModels(ctx context.Context, accountID string, credential Credential) ([]modelRecord, error) {
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, c.apiBase(credential)+modelBuiltinPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Agent-Type", "PromptCenter")
	req.Header.Set("X-Language", Language)
	signRequest(req, nil, credential.Sign())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, newProviderError(resp.StatusCode, string(raw))
	}
	var out struct {
		BuiltinModels []struct {
			ModelID       string `json:"model_id"`
			ModelName     string `json:"model_name"`
			ModelDesc     string `json:"model_desc"`
			ContextWindow int64  `json:"context_window"`
			MaxTokens     int64  `json:"max_tokens"`
		} `json:"builtinModels"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse codearts builtin models: %w", err)
	}
	records := make([]modelRecord, 0, len(out.BuiltinModels))
	for _, model := range out.BuiltinModels {
		id := firstNonEmpty(model.ModelID, model.ModelName)
		if id == "" {
			continue
		}
		records = append(records, modelRecord{
			ID: id, Name: id, Description: model.ModelDesc,
			ContextWindow: model.ContextWindow, MaxTokens: model.MaxTokens,
		})
	}
	return records, nil
}

// fetchBenefitModels reads the limited-benefit gateway config and, matching the
// official client opening the model menu, claims the benefit idempotently.
func (c *Client) fetchBenefitModels(ctx context.Context, accountID string, credential Credential) ([]modelRecord, error) {
	if err := c.claimBenefit(ctx, accountID, credential); err != nil {
		// A claim failure must not hide the rest of the catalog; a later chat
		// will surface benefit-not-found as model_not_available.
		_ = err
	}
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, c.benefitBase+benefitConfig, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Language", Language)
	signRequest(req, nil, credential.Sign())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, newProviderError(resp.StatusCode, string(raw))
	}
	var out struct {
		ErrorCode string `json:"error_code"`
		Result    struct {
			Models []struct {
				ModelID       string `json:"model_id"`
				ModelName     string `json:"model_name"`
				ModelDesc     string `json:"model_desc"`
				ContextWindow int64  `json:"context_window"`
				MaxTokens     int64  `json:"max_tokens"`
			} `json:"models"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse codearts benefit config: %w", err)
	}
	if out.ErrorCode != "" && out.ErrorCode != "0000" {
		return nil, fmt.Errorf("codearts benefit config error %s", out.ErrorCode)
	}
	records := make([]modelRecord, 0, len(out.Result.Models))
	for _, model := range out.Result.Models {
		id := firstNonEmpty(model.ModelID, model.ModelName)
		if id == "" {
			continue
		}
		records = append(records, modelRecord{
			ID: id, Name: id, Description: model.ModelDesc,
			ContextWindow: model.ContextWindow, MaxTokens: model.MaxTokens, Benefit: true,
		})
	}
	return records, nil
}

func (c *Client) claimBenefit(ctx context.Context, accountID string, credential Credential) error {
	body := []byte("{}")
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, c.benefitBase+benefitClaim, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Language", Language)
	signRequest(req, body, credential.Sign())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return newProviderError(resp.StatusCode, string(raw))
	}
	return nil
}

// signedGet sends a signed GET and returns the body.
func (c *Client) signedGet(ctx context.Context, accountID, endpoint string, credential Credential, agentCenter bool) ([]byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, metadataTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Language", Language)
	if agentCenter {
		req.Header.Set("Agent-Type", "AgentCenter")
	}
	signRequest(req, nil, credential.Sign())
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return nil, newProviderError(resp.StatusCode, string(raw))
	}
	return raw, nil
}
