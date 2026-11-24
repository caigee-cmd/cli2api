package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type PrepareError struct {
	Status  int
	Code    string
	Message string
}

func (e *PrepareError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type ModelContextStore interface {
	GetModelContext(ctx context.Context, modelID string) (int, bool, error)
}

type CatalogPreparer interface {
	EnsureModelCatalogs(ctx context.Context, force bool)
}

// AggregationResolver resolves a logical model ID to a read-only snapshot of
// its provider/model members. Implemented by control.Aggregations and injected
// by the gateway. The executor never imports control directly.
type AggregationResolver interface {
	Resolve(ctx context.Context, id string, allowedProviders map[string]bool) (accounts.Aggregation, error)
}

type RequestStarter interface {
	Start(entry accounts.RequestLog)
}

// IdentityAPIKeyID is the console API key id that should be attributed to a
// request, or "" when the caller is not using a named key. Console-key traffic
// stays unattributed so the usage/logs key filter can still show it as one
// bucket.
func IdentityAPIKeyID(identity auth.Identity) string {
	if identity.Kind != auth.KindKey {
		return ""
	}
	return strings.TrimSpace(identity.KeyID)
}

type PrepareInput struct {
	Context           context.Context
	Request           translate.ChatRequest
	NativeRequest     *translate.NativeResponsesRequest
	Identity          auth.Identity
	PreferAccount     string
	SessionHeader     string
	ConversationID    string
	CrossProviderPool bool
	// ModelAggregation lets a key call bare aggregate aliases even when the
	// cross-provider pool setting is off. Only account-defined aliases qualify.
	ModelAggregation bool
	Now              time.Time
	NewRequestID     func() string
	ModelContexts    ModelContextStore
	Catalogs         CatalogPreparer
	Aggregations     AggregationResolver
	Logs             RequestStarter
}

type PreparedRequest struct {
	Context        context.Context
	RequestID      string
	Started        time.Time
	Request        translate.ChatRequest
	NativeRequest  *translate.NativeResponsesRequest
	PublicModel    string
	ProviderFilter string
	Prefer         string
}

func (e ChatExecutor) Prepare(in PrepareInput) (PreparedRequest, error) {
	if in.Context == nil {
		in.Context = context.Background()
	}
	request := in.Request
	publicModel := request.Model
	if in.NativeRequest != nil {
		publicModel = in.NativeRequest.Model()
		request.Model = publicModel
		request.Stream = in.NativeRequest.Stream()
	}
	if in.ModelAggregation {
		// An aggregation key's contract is stricter than the shared pool: it may
		// only call the provider-neutral aliases its own accounts define. A real
		// catalog id or a provider-prefixed id would leak the split view back.
		if strings.Contains(strings.TrimSpace(publicModel), "/") || !e.servesAggregationAlias(publicModel, in.Identity) {
			return PreparedRequest{}, &PrepareError{
				Status:  400,
				Code:    "model_not_aggregated",
				Message: "this API key only accepts its aggregated model names; call /v1/models for the list",
			}
		}
	} else if RejectsBareModel(publicModel, in.CrossProviderPool) {
		return PreparedRequest{}, &PrepareError{
			Status:  400,
			Code:    "provider_prefix_required",
			Message: "cross-provider model pool is disabled; use a provider-prefixed model ID such as qoder/glm-5.2",
		}
	}
	// Resolve a logical aggregation model into an immutable per-request plan.
	// This happens once, before any Pool lock, and the plan is carried in the
	// context so routing uses a stable snapshot (no DB/facade access under lock,
	// no cross-request mutable state).
	var aggregatePlan *AggregatePlan
	if in.Aggregations != nil && (in.CrossProviderPool || in.ModelAggregation) && ProviderPrefix(publicModel) == "" {
		if plan, ok := e.resolveAggregation(in.Context, in.Aggregations, publicModel, in.Identity.AllowedProviders); ok {
			aggregatePlan = plan
		}
	}

	providerFilter, modelRegion := ResolveProviderFilter(&request, in.CrossProviderPool || in.ModelAggregation)
	prefer := strings.TrimSpace(in.PreferAccount)
	providerFilter = ApplyPinnedProviderFilter(e.Pool, providerFilter, publicModel, prefer)
	if providerFilter != "" && !in.Identity.AllowsProvider(providerFilter) {
		return PreparedRequest{}, &PrepareError{
			Status:  403,
			Code:    "provider_not_allowed",
			Message: "This API key cannot use provider " + providerFilter,
		}
	}
	if in.NativeRequest == nil {
		if err := ApplyModelContextDefaults(in.Context, in.ModelContexts, &request, providerFilter); err != nil {
			return PreparedRequest{}, &PrepareError{Status: 500, Code: "model_setting_failed", Message: err.Error()}
		}
	}
	if in.Catalogs != nil {
		in.Catalogs.EnsureModelCatalogs(in.Context, false)
	}
	newID := in.NewRequestID
	if newID == nil {
		newID = accounts.NewRequestID
	}
	requestID := newID()
	started := in.Now
	if started.IsZero() {
		started = time.Now().UTC()
	} else {
		started = started.UTC()
	}
	requestedReasoning := RequestedReasoningLevel(request)
	if in.NativeRequest != nil {
		if level := providers.NormalizeReasoningLevel(in.NativeRequest.RequestedReasoningEffort()); level != "" {
			requestedReasoning = level
		}
	}
	if in.Logs != nil {
		in.Logs.Start(accounts.RequestLog{
			ID:                  requestID,
			CreatedAt:           started,
			Stream:              request.Stream,
			Status:              accounts.RequestStatusStarted,
			RequestedModel:      firstNonEmpty(publicModel, request.Model),
			RequestedReasoning:  requestedReasoning,
			APIKeyID:            IdentityAPIKeyID(in.Identity),
			MessageCount:        len(request.Messages),
			EmptyMessageIndexes: translate.EmptyMessageIndexes(request.Messages),
			MessageRoles:        translate.MessageRoles(request.Messages),
		})
	}
	effectiveAllow := in.Identity.AllowedProviders
	if aggregatePlan != nil && accounts.IsAggregationOnly(in.Identity.AllowedProviders) {
		// The sentinel grant must not filter real member accounts at the pool
		// layer; membership is already constrained by the resolved plan.
		effectiveAllow = nil
	}
	ctx := WithAllowedProviders(WithRequestID(in.Context, requestID), effectiveAllow)
	sessionKey := sessionKeyFor(in.SessionHeader, in.ConversationID, in.Identity, request)
	if sessionKey == "" && in.NativeRequest != nil {
		sessionKey = NativeSessionKeyFor(in.NativeRequest, in.Identity)
	}
	// Sharding is a fallback for requests that have no conversation id and
	// no content seed (a one-shot with no user anchor). It must not replace
	// ContentSessionSeed: that fingerprint already pins one chat to one
	// account, and the shard key drops the first user message, so different
	// chats that share a system prefix would collapse onto one bucket.
	if sessionKey == "" {
		if shard := ShardFallbackKey(in.Identity, request); shard != "" {
			sessionKey = shard
		}
	}
	if sessionKey != "" {
		ctx = WithSessionKey(ctx, sessionKey)
		ctx = WithConversation(ctx, sessionKey)
	}
	if modelRegion != "" {
		ctx = WithRegionFilter(ctx, modelRegion)
	}
	if requestedReasoning != "" {
		log.Printf("request reasoning request_id=%q model=%q level=%q", requestID, request.Model, requestedReasoning)
	}
	if aggregatePlan != nil {
		ctx = WithAggregatePlan(ctx, aggregatePlan)
	}
	if sessionKey != "" && sessionAffinityDebugOn() {
		source := "content-seed"
		if strings.TrimSpace(in.SessionHeader) != "" {
			source = "header"
		} else if strings.TrimSpace(in.ConversationID) != "" {
			source = "conversation"
		} else if sessionKeyFor("", "", in.Identity, request) == "" && MessageStatShards() > 0 {
			source = "shard"
		}
		// The component hashes answer the question a bare key cannot: when a
		// chat re-anchors every turn, which part of the request moved?
		parts := translate.SeedComponents(request)
		log.Printf("session affinity request_id=%q source=%q key=%q model=%q tools=%t msgs=%d",
			requestID, source, shortSessionKey(sessionKey), request.Model, len(request.Tools) > 0, len(request.Messages))
		log.Printf("session affinity components request_id=%q key=%q model_hash=%q tools_hash=%q prefix_hash=%q first_user_hash=%q",
			requestID, shortSessionKey(sessionKey), parts.Model, parts.Tools, parts.Prefix, parts.FirstUser)
	}
	return PreparedRequest{
		Context:        ctx,
		RequestID:      requestID,
		Started:        started,
		Request:        request,
		NativeRequest:  in.NativeRequest,
		PublicModel:    publicModel,
		ProviderFilter: providerFilter,
		Prefer:         prefer,
	}, nil
}

// resolveAggregation attempts to resolve a bare public model ID as a logical
// aggregation model. It returns false for ordinary models so the normal route
// path is unchanged. Provider grants are reduced to a family set used to
// filter members; an empty allowlist means "all providers allowed" (nil map).
func (e ChatExecutor) resolveAggregation(ctx context.Context, resolver AggregationResolver, model string, grants []string) (*AggregatePlan, bool) {
	allowed := map[string]bool(nil)
	// An aggregation-only key must not filter members by its own sentinel
	// grant (that would drop every real provider). nil means "no member
	// filtering"; the pool-level allowlist is cleared for these requests too.
	if accounts.IsAggregationOnly(grants) {
		allowed = nil
	} else if len(grants) > 0 {
		allowed = make(map[string]bool, len(grants))
		for _, entry := range grants {
			// ParseProviderGrant already lowercases and trims; do not pre-fold
			// here so malformed entries keep their strict fail-closed behavior.
			grant, err := accounts.ParseProviderGrant(entry)
			if err != nil || grant.Provider == accounts.AggregationProvider {
				continue
			}
			allowed[grant.Provider] = true
		}
	}
	agg, err := resolver.Resolve(ctx, model, allowed)
	if err != nil || len(agg.Members) == 0 {
		return nil, false
	}
	return NewAggregatePlan(agg), true
}

// providerPrefixes are the provider segments a client may pin with a leading
// "<provider>/" model id. Command Code's own model ids are org-namespaced
// ("deepseek/…", "moonshotai/…"), so its provider prefix sits in front of an
// already-namespaced id ("command/deepseek/…").
var providerPrefixes = []string{"qoder/", "workbuddy/", "trae/", "devin/", "command/", "codex/", "zhipu/", "qwenwork/", "kimi/", "codearts/", "kiro/", "meta/", "muse/", "doubaowork/", "minimaxcode/"}

// regionDashSuffixes are the region qualifiers that may be appended to a
// provider prefix with a dash: "workbuddy-cn/", "workbuddy-gl/",
// "workbuddy-auto/". "-auto/" is the opt-out: it pins the provider family
// while letting the pool pick the region (the lastRegion latch in pool.go).
var regionDashSuffixes = []string{"-cn/", "-gl/", "-auto/"}

// regionAliases maps shorthand region tags to canonical region IDs.
// "auto" maps to "" (no region pin), which triggers the pool's automatic
// region selection instead of a fixed region.
var regionAliases = map[string]string{
	"cn":   "cn",
	"gl":   "global",
	"auto": "",
}

// regionTag extracts the bare region tag from a dash suffix like "-cn/".
func regionTag(suffix string) string {
	return strings.TrimSuffix(strings.TrimPrefix(suffix, "-"), "/")
}

func ProviderPrefix(model string) string {
	model = strings.TrimSpace(model)
	for _, prefix := range providerPrefixes {
		if strings.HasPrefix(model, prefix) {
			return strings.TrimSuffix(prefix, "/")
		}
		base := strings.TrimSuffix(prefix, "/")
		for _, rs := range regionDashSuffixes {
			if strings.HasPrefix(model, base+rs) {
				return base
			}
		}
	}
	return ""
}

func RejectsBareModel(model string, crossProviderPool bool) bool {
	return strings.TrimSpace(model) != "" && !crossProviderPool && ProviderPrefix(model) == ""
}

// servesAggregationAlias reports whether a granted account defines this public
// alias and can actually serve its target. The model list uses the same
// account view, so an aggregation key cannot call a name it cannot see.
func (e ChatExecutor) servesAggregationAlias(model string, identity auth.Identity) bool {
	if e.Pool == nil {
		return false
	}
	want := routeModel(model)
	if want == "" || want == "auto" {
		return false
	}
	for _, item := range e.Pool.Items() {
		if !identity.AllowsProvider(item.Provider) || !identity.AllowsProviderRegion(item.Provider, item.Region) {
			continue
		}
		if strings.TrimSpace(item.ModelAliases[want]) == "" {
			continue
		}
		if itemHasModel(item, want) {
			return true
		}
	}
	return false
}

// ResolveProviderFilter parses the provider prefix (and an optional region
// qualifier) from the model id, strips both from req.Model, and returns the
// provider family and the canonical region ("" when no region was specified).
//
// Supported forms:
//   - "workbuddy/glm-5.3-flash"     → provider=workbuddy, region="" (no region pin; the pool picks)
//   - "qoder-auto/glm-5.2"          → provider=qoder, region="" (explicit auto: same as the bare prefix)
//   - "workbuddy-cn/glm-5.3-flash"  → provider=workbuddy, region=cn
//   - "workbuddy-gl/glm-5.3-flash"  → provider=workbuddy, region=global
//
// The region coexists with API-key grants: routeBaseMatches enforces both
// independently, so a prefix that conflicts with a grant yields no candidates.
func ResolveProviderFilter(req *translate.ChatRequest, crossProviderPool bool) (provider, region string) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return "", ""
	}
	for _, prefix := range providerPrefixes {
		base := strings.TrimSuffix(prefix, "/")
		// Region-qualified dash form: "workbuddy-cn/model"
		for _, rs := range regionDashSuffixes {
			if strings.HasPrefix(model, base+rs) {
				req.Model = strings.TrimPrefix(model, base+rs)
				return accounts.NormalizeProviderFamily(base), regionAliases[regionTag(rs)]
			}
		}
		// Standard form: "workbuddy/model"
		if strings.HasPrefix(model, prefix) {
			req.Model = strings.TrimPrefix(model, prefix)
			return accounts.NormalizeProviderFamily(base), ""
		}
	}
	if crossProviderPool {
		return "", ""
	}
	return "qoder", ""
}

func ApplyPinnedProviderFilter(pool *Pool, providerFilter, publicModel, prefer string) string {
	if prefer == "" || pool == nil {
		return providerFilter
	}
	if ProviderPrefix(publicModel) != "" {
		return providerFilter
	}
	item, ok := pool.ByID(prefer)
	if !ok {
		return providerFilter
	}
	pinned := accounts.NormalizeProviderFamily(item.Provider)
	if providerFilter == "" || providerFilter == pinned {
		return providerFilter
	}
	return pinned
}

func ApplyModelContextDefaults(ctx context.Context, store ModelContextStore, req *translate.ChatRequest, providerFilter string) error {
	if store == nil || req == nil || strings.TrimSpace(req.Model) == "" {
		return nil
	}
	if providerFilter != "" && providerFilter != "qoder" {
		return nil
	}
	// Qoder chat must not carry a window switch in `parameters`: the gateway
	// rejects `contextWindow`/`context_length` there with a context-length error.
	// The selected window is forwarded as `model_config.max_input_tokens`
	// instead, which the Qoder chat body already accepts (so 1M is honoured),
	// so only that field is populated here.
	contextLength, ok, err := store.GetModelContext(ctx, modelContextKey(req.Model))
	if err != nil || !ok {
		return err
	}
	value := json.RawMessage(strconv.Itoa(contextLength))
	if len(req.MaxInputTokens) == 0 {
		req.MaxInputTokens = append(json.RawMessage(nil), value...)
	}
	return nil
}

func modelContextKey(model string) string {
	return accounts.CanonicalModelID(model)
}

func NativeSessionKeyFor(req *translate.NativeResponsesRequest, identity auth.Identity) string {
	if req == nil || strings.TrimSpace(req.SessionSeed()) == "" {
		return ""
	}
	namespace := "console"
	if identity.Kind == auth.KindKey && identity.KeyID != "" {
		namespace = "key:" + identity.KeyID
	}
	sum := sha256.Sum256([]byte(namespace + "\x00native\x00" + req.SessionSeed()))
	return hex.EncodeToString(sum[:])
}

func SessionKeyFor(header string, identity auth.Identity, req translate.ChatRequest) string {
	return sessionKeyFor(header, "", identity, req)
}

// RequestedReasoningLevel surfaces the reasoning level the client asked for,
// normalized to the provider-neutral vocabulary. Empty when the request did
// not specify one.
func RequestedReasoningLevel(req translate.ChatRequest) string {
	return providers.ParseReasoningIntent(req).Level
}

// sessionAffinityDebugOn mirrors qoder2api-hub's QD_AFFINITY_DEBUG: routing
// diagnostics are opt-in so normal request paths stay quiet.
func sessionAffinityDebugOn() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CLI2API_AFFINITY_DEBUG"))) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}
