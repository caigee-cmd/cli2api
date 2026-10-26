package qoder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder/qauth"
	"github.com/caigee-cmd/cli2api/internal/translate"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

// Direct talks to the Qoder chat gateway from this process. It does not load
// the CLI bundle. When an in-process Bridge is available (PAT-backed account),
// it resolves credentials and fresh session tokens from the Bridge; legacy
// accounts without a PAT fall back to the stored CLI user_blob.
type Direct struct {
	store      CredentialStore
	region     func(ctx context.Context, accountID string) (string, error)
	proxy      func(ctx context.Context, accountID string) (string, error)
	transports proxyutil.TransportCache
	bridgeOf   func(ctx context.Context, accountID string) (*Bridge, error)
	http       *http.Client
	now      func() time.Time
	// ttfb bounds how long ChatStream waits for the upstream response head
	// and, after a 200, for the first meaningful SSE data line
	// (ensureMeaningfulUpstream). Zero falls back to defaultFirstByteTimeout.
	ttfb time.Duration
	// guard is the optional per-account traffic limiter (default off).
	guard *trafficGuard
}

func NewDirect(store CredentialStore, region func(context.Context, string) (string, error)) *Direct {
	return &Direct{
		store:  store,
		region: region,
		http:   &http.Client{},
		now:    time.Now,
		ttfb:   resolveFirstByteTimeout(),
		guard:  newTrafficGuard(trafficGuardConfigFromEnv()),
	}
}

func (d *Direct) SetBridgeResolver(fn func(ctx context.Context, accountID string) (*Bridge, error)) {
	if d == nil {
		return
	}
	d.bridgeOf = fn
}

func (d *Direct) SetHTTP(client *http.Client) {
	if d == nil || client == nil {
		return
	}
	d.http = client
}

func (d *Direct) SetProxy(fn func(ctx context.Context, accountID string) (string, error)) {
	if d == nil {
		return
	}
	d.proxy = fn
}

func (d *Direct) httpClient(ctx context.Context, accountID string) (*http.Client, error) {
	if d == nil {
		return nil, fmt.Errorf("qoder direct client is nil")
	}
	base := d.http
	if base == nil {
		base = &http.Client{}
	}
	if d.proxy == nil {
		return base, nil
	}
	rawProxy, err := d.proxy(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(rawProxy) == "" {
		return base, nil
	}

	transport, err := d.transports.Get(rawProxy)
	if err != nil {
		return nil, fmt.Errorf("configure proxy transport: %w", err)
	}
	if transport == nil {
		return base, nil
	}

	client := *base
	client.Transport = transport
	return &client, nil
}

type directPrepared struct {
	request *http.Request
	model   string
	level   string
}

func (d *Direct) prepare(ctx context.Context, accountID string, req translate.ChatRequest) (directPrepared, error) {
	if d == nil {
		return directPrepared{}, fmt.Errorf("qoder direct client is not configured")
	}
	var user CLIUser
	var session *Session
	var region string
	var err error
	var catalogOf func(context.Context) ([]map[string]any, error)

	// 1. In-process Bridge path: when the account has a stored PAT, the Bridge
	// manages the session lifecycle (fresh securityOauthToken + refreshToken)
	// and stable machine identity.
	if d.bridgeOf != nil {
		if b, bridgeErr := d.bridgeOf(ctx, accountID); bridgeErr == nil {
			catalogOf = b.RoutingCatalog
			if err := b.EnsureFreshSession(ctx); err != nil {
				return directPrepared{}, fmt.Errorf("qoder ensure fresh session: %w", err)
			}
			ident := b.currentIdentity()
			if ident != nil {
				user = CLIUser{
					Name:               ident.Name,
					UID:                ident.UID,
					Aid:                ident.Aid,
					YxUID:              ident.YxUID,
					OrganizationID:     ident.OrganizationID,
					OrganizationName:   ident.OrganizationName,
					UserType:           ident.UserType,
					SecurityOauthToken: ident.SecurityOauthToken,
					RefreshToken:       ident.RefreshToken,
				}
				session, err = NewSession(user)
				if err != nil {
					return directPrepared{}, err
				}
				session.MachineID = b.MachineID()
				session.MachineToken = b.MachineToken()
				session.MachineType = b.MachineType()
				if b.Region != nil && b.Region.Name != "" {
					region = b.Region.Name
				}
			}
		}
	}

	// 2. Legacy fallback: account has no PAT, load encrypted CLI user blob.
	if session == nil {
		if d.store == nil {
			return directPrepared{}, fmt.Errorf("qoder direct client is not configured")
		}
		credential, err := d.store.LoadCredential(ctx, accountID)
		if err != nil {
			return directPrepared{}, err
		}
		user, err = DecryptCLIUser(credential.UserBlob, credential.MachineID)
		if err != nil {
			return directPrepared{}, err
		}
		session, err = NewSession(user)
		if err != nil {
			return directPrepared{}, err
		}
	}

	if region == "" {
		region = "global"
		if d.region != nil {
			region, err = d.region(ctx, accountID)
			if err != nil {
				return directPrepared{}, err
			}
		}
	}
	var entry map[string]any
	if catalogOf != nil {
		req, entry, err = resolveCatalogModel(ctx, catalogOf, req)
		if err != nil {
			return directPrepared{}, err
		}
	}
	plain := BuildPlainChatBody(req, d.now())
	applyCatalogEntry(plain, req, entry)
	if user.UserType != "" {
		plain["aliyun_user_type"] = user.UserType
	}
	model, _ := plain["model_config"].(map[string]any)
	modelKey, _ := model["key"].(string)
	body, err := json.Marshal(plain)
	if err != nil {
		return directPrepared{}, err
	}
	endpoint := chatEndpoint(region)
	encoded := EncodeBody(body)
	headers, err := session.Sign(encoded, endpoint, d.now())
	if err != nil {
		return directPrepared{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(encoded))
	if err != nil {
		return directPrepared{}, err
	}
	for key, value := range headers {
		httpReq.Header.Set(key, value)
	}
	if modelKey != "" {
		httpReq.Header.Set("X-Model-Key", modelKey)
		httpReq.Header.Set("X-Model-Source", "system")
	}
	return directPrepared{request: httpReq, model: modelKey, level: resolvedReasoningLevel(req)}, nil
}

func (d *Direct) ChatNonStream(ctx context.Context, accountID string, req translate.ChatRequest) (providers.ChatOutcome, error) {
	prepared, err := d.prepare(ctx, accountID, req)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	release, err := d.guard.acquire(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer release()
	client, err := d.httpClient(ctx, accountID)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	resp, err := client.Do(prepared.request)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	if resp.StatusCode >= 300 {
		return providers.ChatOutcome{}, upstreamStatusError(resp.StatusCode, raw)
	}
	outcome, err := outcomeFromUpstreamSSE(prepared.model, raw)
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	outcome.ReasoningLevel = prepared.level
	return outcome, nil
}

func (d *Direct) ChatStream(ctx context.Context, accountID string, req translate.ChatRequest) (*http.Response, providers.ResolvedChat, error) {
	prepared, err := d.prepare(ctx, accountID, req)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	release, err := d.guard.acquire(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	client, err := d.httpClient(ctx, accountID)
	if err != nil {
		return nil, providers.ResolvedChat{}, err
	}
	resp, err := doWithFirstByteTimeout(client, prepared.request, d.ttfb)
	if err != nil {
		release()
		return nil, providers.ResolvedChat{}, err
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		release()
		return nil, providers.ResolvedChat{}, upstreamStatusError(resp.StatusCode, raw)
	}
	resp.Body = releaseOnClose{ReadCloser: resp.Body, release: release}
	guarded, err := ensureMeaningfulUpstream(resp, d.ttfb)
	if err != nil {
		// ensureMeaningfulUpstream closes the body on failure; no client bytes
		// have been written yet, so the executor can still fail over.
		release()
		return nil, providers.ResolvedChat{}, err
	}
	return rewriteUpstreamSSE(guarded, prepared.model), providers.ResolvedChat{ReasoningLevel: prepared.level}, nil
}

// defaultQueueRetryAfter is used when a 10605 queue rejection carries no
// retryAfterSeconds hint.
const defaultQueueRetryAfter = 5 * time.Second

// clientVersionTooLowMessage explains Qoder's HTTP 430 hard gate.
const clientVersionTooLowMessage = "client version too low (HTTP 430): Qoder requires a newer client version; upgrade cli2api. This is not an account or network problem."

func upstreamStatusError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = fmt.Sprintf("qoder upstream status %d", status)
	}
	// 10605 is model-level queueing, not an account fault (qoder2api-hub
	// 6b7f65b / 67aa3ce): no cooldown, no dead mark, no failover (which would
	// rebind the session to another account); surface Retry-After instead.
	if busy := qauth.ParseBusyEnvelope(status, body); busy != nil {
		retry := busy.RetryAfter
		if retry <= 0 {
			retry = defaultQueueRetryAfter
		}
		return &providers.Error{
			Kind: accounts.KindRateLimit, Status: http.StatusTooManyRequests,
			Code: providers.CodeUpstreamQueued, RetryAfter: retry,
			Message: "qoder upstream queued (10605): " + busy.Message,
		}
	}
	if status == qauth.StatusClientVersionTooLow {
		return &providers.Error{
			Kind: accounts.KindUnavailable, Status: http.StatusBadGateway,
			Code: providers.CodeClientVersionTooLow, Message: clientVersionTooLowMessage,
		}
	}
	kind := accounts.KindUnavailable
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = accounts.KindAuth
	case http.StatusTooManyRequests:
		kind = accounts.KindRateLimit
	case http.StatusBadRequest:
		kind = accounts.KindInvalidRequest
	}
	return &providers.Error{Kind: kind, Status: status, Message: message}
}

// emptyUpstreamError mirrors the worker path's upstream_empty classification.
// The executor cools the account down and fails over instead of relaying a
// spec-invalid completion (no content, no finish_reason) that strict OpenAI
// clients reject with "Stream ended without finish_reason".
func emptyUpstreamError() error {
	return &providers.Error{
		Kind:    accounts.KindUnavailable,
		Status:  http.StatusBadGateway,
		Code:    "upstream_empty",
		Message: "upstream returned empty content (possible context overflow / silent reject)",
	}
}

// ensureMeaningfulUpstream peeks the upstream SSE body before anything is
// relayed to the client. Qoder's gateway sometimes answers 200 with heartbeat
// frames only (no content, no finish_reason, no usage, no error) and closes
// the stream; relaying that verbatim produced a spec-invalid empty
// completion. Peek before the first client byte so the failure can still be
// classified and retried on another account. On success the buffered bytes
// are replayed ahead of the remaining body, preserving real-time streaming.
//
// timeout bounds how long to wait for a meaningful data line after the
// response head arrives. Zero falls back to defaultFirstByteTimeout so the
// bound matches QODER_STREAM_TTFB_MS / Direct.ttfb. A stuck heartbeat-only
// upstream closes the body and returns emptyUpstreamError so the executor
// can fail over instead of hanging forever (doWithFirstByteTimeout only
// covers headers).
func ensureMeaningfulUpstream(resp *http.Response, timeout time.Duration) (*http.Response, error) {
	if resp == nil || resp.Body == nil {
		return nil, emptyUpstreamError()
	}
	if timeout <= 0 {
		timeout = defaultFirstByteTimeout
	}

	// Close at most once: the timeout timer and the read-error path both need
	// to finish the body, but cancelOnCloseBody.Close is not concurrency-safe.
	// Success stops the timer before returning the live peeked body; if the
	// closer already won the race, treat it as emptyUpstreamError rather than
	// handing the caller a half-closed stream.
	var closeOnce sync.Once
	closeBody := func() {
		closeOnce.Do(func() { _ = resp.Body.Close() })
	}

	var expired atomic.Bool
	timer := time.AfterFunc(timeout, func() {
		expired.Store(true)
		closeBody()
	})
	stopTimer := func() { _ = timer.Stop() }

	br := bufio.NewReader(resp.Body)
	var pending bytes.Buffer
	for {
		line, readErr := br.ReadString('\n')
		if line != "" {
			pending.WriteString(line)
			if data, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok && meaningfulDataLine(strings.TrimSpace(data)) {
				stopTimer()
				if expired.Load() {
					closeBody()
					return nil, emptyUpstreamError()
				}
				resp.Body = peekedBody{Reader: io.MultiReader(&pending, br), Closer: resp.Body}
				return resp, nil
			}
		}
		if readErr != nil {
			stopTimer()
			closeBody()
			if expired.Load() {
				return nil, emptyUpstreamError()
			}
			if errors.Is(readErr, io.EOF) {
				return nil, emptyUpstreamError()
			}
			return nil, &providers.Error{
				Kind:    accounts.KindUnavailable,
				Status:  http.StatusBadGateway,
				Code:    "upstream_stream_interrupted",
				Message: readErr.Error(),
			}
		}
	}
}

// peekedBody keeps the original body's Close wired to the upstream response
// while prepending the peeked bytes.
type peekedBody struct {
	io.Reader
	io.Closer
}

// meaningfulDataLine reports whether a single upstream SSE data line carries
// any payload a relayed completion needs: content, reasoning, tool calls, a
// terminal finish_reason, usage, or an error. Heartbeat and empty-delta
// frames are not meaningful.
func meaningfulDataLine(payload string) bool {
	if payload == "" || payload == "[DONE]" {
		return false
	}
	var outer map[string]any
	if json.Unmarshal([]byte(payload), &outer) != nil {
		return false
	}
	body := outer
	if nested, ok := outer["body"]; ok {
		switch typed := nested.(type) {
		case string:
			var inner map[string]any
			if json.Unmarshal([]byte(typed), &inner) == nil {
				body = inner
			}
		case map[string]any:
			body = typed
		}
	}
	frame := frameFromBody(body)
	return frame.content != "" || frame.reasoning != "" || len(frame.tools) > 0 || frame.finish != "" || frame.usage != nil || frame.errText != ""
}

// BuildPlainChatBody is the plaintext agent_chat_generation document. The
// gateway expects this shape before Encode=1; it is not the OpenAI body the
// worker used to accept.
func BuildPlainChatBody(req translate.ChatRequest, now time.Time) map[string]any {
	if now.IsZero() {
		now = time.Now()
	}
	var systemParts []string
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, message := range req.Messages {
		text := strings.TrimSpace(translate.ContentToString(message.Content))
		if message.Role == "system" || message.Role == "developer" {
			if text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		item := map[string]any{"role": message.Role, "content": normalizeMessageContent(message)}
		if message.Name != "" {
			item["name"] = message.Name
		}
		if message.ToolCallID != "" {
			item["tool_call_id"] = message.ToolCallID
		}
		if message.ReasoningContent != "" {
			item["reasoning_content"] = message.ReasoningContent
		}
		if len(message.ToolCalls) > 0 {
			item["tool_calls"] = json.RawMessage(message.ToolCalls)
		}
		messages = append(messages, item)
	}
	systemText := strings.Join(systemParts, "\n\n")
	if systemText != "" {
		messages = append([]map[string]any{{"role": "system", "content": systemText}}, messages...)
	}
	if len(messages) == 0 {
		messages = []map[string]any{{"role": "user", "content": "ping"}}
	}
	modelKey := strings.TrimSpace(req.Model)
	if modelKey == "" {
		modelKey = "auto"
	}
	maxTokens := 32000
	if n, ok := rawNumber(req.MaxCompletionTokens); ok && n > 0 {
		maxTokens = n
	} else if n, ok := rawNumber(req.MaxTokens); ok && n > 0 {
		maxTokens = n
	}
	parameters := map[string]any{"max_tokens": maxTokens}
	copyRaw(parameters, "temperature", req.Temperature)
	copyRaw(parameters, "top_p", req.TopP)
	copyRaw(parameters, "stop", req.Stop)
	copyRaw(parameters, "response_format", req.ResponseFormat)
	copyRaw(parameters, "reasoning_effort", req.ReasoningEffort)
	copyRaw(parameters, "reasoning_budget_tokens", req.ReasoningBudgetTokens)
	copyRaw(parameters, "context_length", req.ContextLength)
	copyRaw(parameters, "max_input_tokens", req.MaxInputTokens)
	copyRaw(parameters, "tool_choice", req.ToolChoice)
	if req.ParallelToolCalls != nil {
		parameters["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	level := resolvedReasoningLevel(req)
	reasoning := level != "" && level != "none"
	if level != "" {
		parameters["enable_thinking"] = reasoning
	}
	maxInput := 180000
	if n, ok := rawNumber(req.MaxInputTokens); ok && n > 0 {
		maxInput = n
	}
	prompt := latestUserPrompt(messages)
	name := "chat"
	if prompt != "" {
		name = prompt
	}
	if len(name) > 40 {
		name = name[:40]
	}
	return map[string]any{
		"request_id":     newID(),
		"request_set_id": newID(),
		"chat_record_id": newID(),
		"session_id":     newID(),
		"stream":         true,
		"chat_task":      "FREE_INPUT",
		// chat_context mirrors the reference Qoder client (and the working
		// Qoder-2API-Go): the gateway resolves the thinking channel and the routed
		// model from chat_context.extra.modelConfig. An empty extra block makes
		// the gateway answer without a reasoning_content stream even when the
		// top-level model_config.is_reasoning and parameters.enable_thinking are
		// set, so both copies must carry the model key and reasoning flag.
		"chat_context": map[string]any{
			"text":     map[string]any{"type": "text", "text": prompt},
			"features": []any{},
			"extra": map[string]any{
				"modelConfig":     map[string]any{"key": modelKey, "is_reasoning": reasoning},
				"originalContent": map[string]any{"type": "text", "text": prompt},
			},
			"chatPrompt": "",
			"imageUrls":  nil,
		},
		"is_reply":         false,
		"is_retry":         false,
		"source":           1,
		"version":          "3",
		"agent_id":         "agent_common",
		"task_id":          "common",
		"session_type":     "qodercli",
		"aliyun_user_type": "",
		"model_config": map[string]any{
			"key": modelKey, "display_name": modelKey, "model": "", "format": "openai",
			"is_vl": true, "is_reasoning": reasoning, "api_key": "", "url": "",
			"source": "system", "max_input_tokens": maxInput,
		},
		"custom_model": nil,
		"system":       systemText,
		"messages":     messages,
		"tools":        rawOrEmpty(req.Tools),
		"parameters":   parameters,
		"business": map[string]any{
			"product": "cli", "version": COSYVersion, "type": "agent",
			"id": newID(), "name": name, "begin_at": now.UnixMilli(), "stage": "start",
		},
	}
}

// latestUserPrompt returns the most recent user message's text, which the
// reference client duplicates into chat_context.text and
// chat_context.extra.originalContent. Empty when no user turn carries text.
func latestUserPrompt(messages []map[string]any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		role, _ := messages[i]["role"].(string)
		if role != "user" {
			continue
		}
		if text := strings.TrimSpace(translate.ContentToString(messages[i]["content"])); text != "" {
			return text
		}
	}
	return ""
}

// normalizeMessageContent replaces a nil content on a message that carries
// tool_calls with an empty string.
//
// The gateway pairs each role:"tool" message with the tool_calls of the
// assistant turn ahead of it, and it only registers that pairing when the
// assistant message's `content` is a string. A JSON null — what OpenAI clients
// send for an assistant turn that is pure tool calls — leaves the call
// unregistered, and the following role:"tool" message is then rejected with
//
//	messages with role "tool" must be a response to a preceeding message
//	with "tool_calls"
//
// An empty string is accepted and pairs correctly. Only messages with
// tool_calls are rewritten; every other content shape passes through
// untouched.
func normalizeMessageContent(message translate.ChatMessage) any {
	if len(message.ToolCalls) > 0 && message.Content == nil {
		return ""
	}
	return message.Content
}

func rawNumber(raw json.RawMessage) (int, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	var n int
	if json.Unmarshal(raw, &n) != nil {
		return 0, false
	}
	return n, true
}

func copyRaw(dst map[string]any, key string, raw json.RawMessage) {
	if len(bytes.TrimSpace(raw)) == 0 || string(raw) == "null" {
		return
	}
	dst[key] = json.RawMessage(raw)
}

func rawOrEmpty(raw json.RawMessage) any {
	if len(bytes.TrimSpace(raw)) == 0 {
		return []any{}
	}
	return json.RawMessage(raw)
}

func newID() string {
	id, err := randomUUID()
	if err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	return id
}

type upstreamFrame struct {
	content   string
	reasoning string
	tools     json.RawMessage
	finish    string
	usage     json.RawMessage
	errText   string
	errStatus int
	errKind   string
}

func outcomeFromUpstreamSSE(model string, raw []byte) (providers.ChatOutcome, error) {
	frame, err := collectUpstream(bytes.NewReader(raw))
	if err != nil {
		return providers.ChatOutcome{}, err
	}
	if frame.errText != "" && frame.content == "" && frame.reasoning == "" && len(frame.tools) == 0 {
		kind := frame.errKind
		if kind == "" {
			kind = accounts.KindUnavailable
		}
		status := frame.errStatus
		if status == 0 {
			status = http.StatusBadGateway
		}
		return providers.ChatOutcome{}, &providers.Error{Kind: kind, Status: status, Message: frame.errText}
	}
	// Guard against the same empty-stream failure mode as the streaming path:
	// an upstream body without content, reasoning, tool calls, a terminal
	// finish_reason, or an error frame is a silent upstream reject, not a
	// completed (empty) answer.
	if frame.finish == "" && frame.content == "" && frame.reasoning == "" && len(frame.tools) == 0 {
		return providers.ChatOutcome{}, emptyUpstreamError()
	}
	out := providers.ChatOutcome{
		Model:        model,
		Content:      frame.content,
		Reasoning:    frame.reasoning,
		ToolCalls:    frame.tools,
		FinishReason: frame.finish,
		UsageSource:  "upstream",
	}
	if out.FinishReason == "" {
		if len(out.ToolCalls) > 0 {
			out.FinishReason = "tool_calls"
		} else {
			out.FinishReason = "stop"
		}
	}
	applyUsage(&out, frame.usage)
	return out, nil
}

func applyUsage(out *providers.ChatOutcome, raw json.RawMessage) {
	if len(raw) == 0 {
		out.UsageSource = "estimate"
		return
	}
	var usage struct {
		PromptTokens         int      `json:"prompt_tokens"`
		CompletionTokens     int      `json:"completion_tokens"`
		CacheReadTokens      *int     `json:"cache_read_tokens"`
		CacheWriteTokens     *int     `json:"cache_write_tokens"`
		PromptCacheHitTokens *int     `json:"prompt_cache_hit_tokens"`
		Credits              *float64 `json:"credits"`
		PromptDetails        struct {
			CachedTokens *int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	}
	if json.Unmarshal(raw, &usage) != nil {
		out.UsageSource = "estimate"
		return
	}
	out.PromptTokens = usage.PromptTokens
	out.CompletionTokens = usage.CompletionTokens
	cacheRead := usage.CacheReadTokens
	if cacheRead == nil {
		cacheRead = usage.PromptDetails.CachedTokens
	}
	if cacheRead == nil {
		cacheRead = usage.PromptCacheHitTokens
	}
	out.CacheReadTokens = cacheRead
	out.CacheWriteTokens = usage.CacheWriteTokens
	out.Credits = usage.Credits
}

func collectUpstream(r io.Reader) (upstreamFrame, error) {
	var frame upstreamFrame
	var content, reasoning strings.Builder
	var acc toolCallAccumulator
	err := walkUpstream(r, func(body map[string]any) error {
		if errText, status, kind := upstreamError(body); errText != "" {
			frame.errText = errText
			frame.errStatus = status
			frame.errKind = kind
		}
		if usage, ok := body["usage"]; ok && usage != nil {
			raw, _ := json.Marshal(usage)
			frame.usage = raw
		}
		choices, _ := body["choices"].([]any)
		if len(choices) == 0 {
			return nil
		}
		choice, _ := choices[0].(map[string]any)
		if choice == nil {
			return nil
		}
		if finish, _ := choice["finish_reason"].(string); finish != "" {
			frame.finish = finish
		}
		delta, _ := choice["delta"].(map[string]any)
		message, _ := choice["message"].(map[string]any)
		part := delta
		if part == nil {
			part = message
		}
		if part == nil {
			return nil
		}
		if text, _ := part["content"].(string); text != "" {
			content.WriteString(text)
		}
		if text, _ := part["reasoning_content"].(string); text != "" {
			reasoning.WriteString(text)
		}
		if tools, _ := part["tool_calls"].([]any); len(tools) > 0 {
			acc.add(tools)
		}
		return nil
	})
	frame.content = content.String()
	frame.reasoning = reasoning.String()
	frame.tools = acc.encode()
	return frame, err
}

// toolCallAccumulator merges streamed tool_call deltas by index: id, type
// and function name keep their first non-empty value, arguments concatenate.
type toolCallAccumulator struct {
	order []int
	calls map[int]*accumulatedToolCall
}

type accumulatedToolCall struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Index    int    `json:"index"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (a *toolCallAccumulator) add(tools []any) {
	if a.calls == nil {
		a.calls = map[int]*accumulatedToolCall{}
	}
	for position, rawTool := range tools {
		tool, _ := rawTool.(map[string]any)
		if tool == nil {
			continue
		}
		index := position
		if n, ok := tool["index"].(float64); ok {
			index = int(n)
		}
		call := a.calls[index]
		if call == nil {
			call = &accumulatedToolCall{Index: index}
			a.calls[index] = call
			a.order = append(a.order, index)
		}
		if id, _ := tool["id"].(string); id != "" && call.ID == "" {
			call.ID = id
		}
		if typ, _ := tool["type"].(string); typ != "" && call.Type == "" {
			call.Type = typ
		}
		if fn, _ := tool["function"].(map[string]any); fn != nil {
			if name, _ := fn["name"].(string); name != "" && call.Function.Name == "" {
				call.Function.Name = name
			}
			if args, _ := fn["arguments"].(string); args != "" {
				call.Function.Arguments += args
			}
		}
	}
}

func (a *toolCallAccumulator) encode() json.RawMessage {
	if len(a.order) == 0 {
		return nil
	}
	sort.Ints(a.order)
	out := make([]*accumulatedToolCall, 0, len(a.order))
	for _, index := range a.order {
		out = append(out, a.calls[index])
	}
	raw, _ := json.Marshal(out)
	return raw
}

func upstreamError(body map[string]any) (string, int, string) {
	raw, ok := body["error"]
	if !ok || raw == nil {
		return "", 0, ""
	}
	encoded, _ := json.Marshal(raw)
	var errBody struct {
		Message string `json:"message"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
	}
	_ = json.Unmarshal(encoded, &errBody)
	message := strings.TrimSpace(errBody.Message)
	if message == "" {
		message = strings.TrimSpace(string(encoded))
	}
	kind := accounts.KindUnavailable
	status := http.StatusBadGateway
	code := fmt.Sprint(errBody.Code)
	if code == "insufficient_quota" || strings.Contains(strings.ToLower(message), "quota") {
		kind = accounts.KindQuota
		status = http.StatusTooManyRequests
	}
	return message, status, kind
}

func walkUpstream(r io.Reader, fn func(map[string]any) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			// Explicit terminator: finish without waiting for transport EOF.
			return nil
		}
		if data == "" {
			continue
		}
		var outer map[string]any
		if json.Unmarshal([]byte(data), &outer) != nil {
			continue
		}
		body := outer
		if nested, ok := outer["body"]; ok {
			switch typed := nested.(type) {
			case string:
				var inner map[string]any
				if json.Unmarshal([]byte(typed), &inner) == nil {
					body = inner
				}
			case map[string]any:
				body = typed
			}
		}
		if err := fn(body); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func rewriteUpstreamSSE(resp *http.Response, model string) *http.Response {
	pr, pw := io.Pipe()
	// Both the converter goroutine and the caller may close upstream; Once
	// keeps non-idempotent wrappers (cancelOnCloseBody) race-free.
	closeUpstream := sync.OnceValue(resp.Body.Close)
	go func() {
		defer closeUpstream()
		err := writeOpenAIChunks(pw, resp.Body, model)
		_ = pw.CloseWithError(err)
	}()
	headers := resp.Header.Clone()
	headers.Set("Content-Type", "text/event-stream")
	headers.Del("Content-Length")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     headers,
		Body:       &convertedBody{pr: pr, closeUpstream: closeUpstream},
		Request:    resp.Request,
	}
}

// convertedBody closes the pipe reader so a blocked converter goroutine
// unblocks, and closes the upstream body so the connection is released even
// when the consumer stops reading early.
type convertedBody struct {
	pr            *io.PipeReader
	closeUpstream func() error
}

func (b *convertedBody) Read(p []byte) (int, error) { return b.pr.Read(p) }

func (b *convertedBody) Close() error {
	_ = b.pr.CloseWithError(io.ErrClosedPipe)
	return b.closeUpstream()
}

// newCompletionID returns one OpenAI completion id per request. Clients that
// merge assistant chunks by id must not see two requests share one (#292).
func newCompletionID() string {
	id, err := randomUUID()
	if err != nil {
		return fmt.Sprintf("chatcmpl-qoder-%d", time.Now().UnixNano())
	}
	return "chatcmpl-" + strings.ReplaceAll(id, "-", "")
}

func writeOpenAIChunks(w io.Writer, src io.Reader, model string) error {
	completionID := newCompletionID()
	var sawFinish, sawTools, sawPayload, sawError bool
	var lastUsage any
	var lastChunkEmittedHadUsage bool
	err := walkUpstream(src, func(body map[string]any) error {
		frame := frameFromBody(body)
		if frame.finish != "" {
			sawFinish = true
		}
		if frame.content != "" || frame.reasoning != "" || len(frame.tools) > 0 || frame.usage != nil {
			sawPayload = true
		}
		if len(frame.tools) > 0 {
			sawTools = true
		}
		if frame.errText != "" && frame.content == "" && frame.reasoning == "" && len(frame.tools) == 0 {
			sawError = true
			payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": frame.errText, "type": "api_error"}})
			_, err := fmt.Fprintf(w, "data: %s\n\n", payload)
			return err
		}
		chunk := map[string]any{
			"id": completionID, "object": "chat.completion.chunk", "model": model,
			"choices": []any{map[string]any{"index": 0, "delta": deltaOf(frame), "finish_reason": emptyOrNil(frame.finish)}},
		}
		if len(frame.usage) > 0 {
			var rawUsage any
			if json.Unmarshal(frame.usage, &rawUsage) == nil {
				if usage, ok := rawUsage.(map[string]any); ok {
					normalizePromptCacheUsage(usage)
					chunk["usage"] = usage
					lastUsage = usage
				} else {
					chunk["usage"] = rawUsage
					lastUsage = rawUsage
				}
			}
		} else if frame.finish != "" && lastUsage != nil {
			chunk["usage"] = lastUsage
		}
		lastChunkEmittedHadUsage = chunk["usage"] != nil
		payload, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", payload)
		return err
	})
	if err != nil {
		return err
	}
	if !sawFinish {
		if sawPayload {
			// Upstream streamed real content but was cut before the terminal
			// finish_reason. The bytes are already on the wire, so salvage the
			// completion with a synthesized terminal chunk instead of leaving
			// strict OpenAI clients with "Stream ended without finish_reason".
			finish := "stop"
			if sawTools {
				finish = "tool_calls"
			}
			chunk := map[string]any{
				"id": completionID, "object": "chat.completion.chunk", "model": model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}},
			}
			if lastUsage != nil {
				chunk["usage"] = lastUsage
			}
			payload, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return err
			}
		} else if !sawError {
			// Empty upstream stream that slipped past the pre-relay guard:
			// surface it as an error frame so the gateway classifies and logs
			// the failure instead of reporting a successful empty answer.
			err := emptyUpstreamError()
			payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "type": "api_error"}})
			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				return err
			}
		}
	} else if !lastChunkEmittedHadUsage && lastUsage != nil {
		chunk := map[string]any{
			"id": completionID, "object": "chat.completion.chunk", "model": model,
			"choices": []any{},
			"usage":   lastUsage,
		}
		payload, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, "data: [DONE]\n\n")
	return err
}

func normalizePromptCacheUsage(usage map[string]any) {
	if usage == nil {
		return
	}
	var cachedTokens any
	if ptd, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		cachedTokens = ptd["cached_tokens"]
	}
	if cachedTokens == nil {
		cachedTokens = usage["cache_read_tokens"]
	}
	if cachedTokens == nil {
		cachedTokens = usage["prompt_cache_hit_tokens"]
	}
	if cachedTokens == nil {
		return
	}
	if _, ok := usage["cache_read_tokens"]; !ok {
		usage["cache_read_tokens"] = cachedTokens
	}
	if _, ok := usage["prompt_cache_hit_tokens"]; !ok {
		usage["prompt_cache_hit_tokens"] = cachedTokens
	}
	if _, ok := usage["prompt_tokens_details"]; !ok {
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": cachedTokens}
	}
}

func frameFromBody(body map[string]any) upstreamFrame {
	var frame upstreamFrame
	if errText, status, kind := upstreamError(body); errText != "" {
		frame.errText = errText
		frame.errStatus = status
		frame.errKind = kind
	}
	if usage, ok := body["usage"]; ok && usage != nil {
		raw, _ := json.Marshal(usage)
		frame.usage = raw
	}
	choices, _ := body["choices"].([]any)
	if len(choices) == 0 {
		return frame
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return frame
	}
	if finish, _ := choice["finish_reason"].(string); finish != "" {
		frame.finish = finish
	}
	delta, _ := choice["delta"].(map[string]any)
	message, _ := choice["message"].(map[string]any)
	part := delta
	if part == nil {
		part = message
	}
	if part == nil {
		return frame
	}
	if text, _ := part["content"].(string); text != "" {
		frame.content = text
	}
	if text, _ := part["reasoning_content"].(string); text != "" {
		frame.reasoning = text
	}
	if tools, _ := part["tool_calls"].([]any); len(tools) > 0 {
		raw, _ := json.Marshal(tools)
		frame.tools = raw
	}
	return frame
}

func deltaOf(frame upstreamFrame) map[string]any {
	delta := map[string]any{}
	if frame.content != "" {
		delta["content"] = frame.content
	}
	if frame.reasoning != "" {
		delta["reasoning_content"] = frame.reasoning
	}
	if len(frame.tools) > 0 {
		var tools any
		if json.Unmarshal(frame.tools, &tools) == nil {
			delta["tool_calls"] = tools
		}
	}
	return delta
}

func emptyOrNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}
