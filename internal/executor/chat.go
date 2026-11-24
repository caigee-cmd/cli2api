package executor

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

type providerRegistry = providers.Registry

type AttemptHook func(accounts.RequestAttempt)

type ChatExecutor struct {
	Pool      *Pool
	WorkerKey string
	// WorkerKeySource, when set, supplies the live key shared by executor copies.
	WorkerKeySource func() string
	HTTPClient      *http.Client
	Providers       *providerRegistry
	OnAttempt       AttemptHook
	MaxAttempts     int
	SessionAffinity *SessionAffinity
	// OnInFlightAcquire/Release mirror Pool.Acquire/Release onto RequestRecorder
	// open-request tracking (same plane as recent_completions). Optional.
	OnInFlightAcquire func(requestID, accountID string)
	OnInFlightRelease func(requestID, accountID string)
}

type ChatResult struct {
	Model            string
	Content          string
	Reasoning        string
	ToolCalls        json.RawMessage
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  *int
	CacheWriteTokens *int
	CachedTokens     *int
	UsageSource      string
	Credits          *float64
	ConsumedCredits  *float64
	AccountID        string
	Provider         string
	AttemptCount     int
	RawNote          string
	Routing          string
	ReasoningLevel   string
}

type StreamResult struct {
	Response  *http.Response
	AccountID string
	// PoolAccountID is the pool routing key for AccountID. A worker may
	// rewrite the response account label, but session affinity and cooldown
	// bookkeeping must always address the pool item.
	PoolAccountID  string
	Provider       string
	AttemptCount   int
	TTFBMs         int
	Routing        string
	ReasoningLevel string
	// NativeResponses marks the body as an upstream OpenAI Responses SSE stream
	// that /v1/responses can relay verbatim instead of translating.
	NativeResponses bool
}

type NativeResponseResult struct {
	Response       json.RawMessage
	AccountID      string
	Provider       string
	AttemptCount   int
	Routing        string
	ReasoningLevel string
	FinishReason   string
	PromptTokens   *int
	OutputTokens   *int
	CachedTokens   *int
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	if strings.TrimSpace(id) == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type allowedProvidersKey struct{}

func WithAllowedProviders(ctx context.Context, providers []string) context.Context {
	if len(providers) == 0 {
		return ctx
	}
	copied := append([]string{}, providers...)
	return context.WithValue(ctx, allowedProvidersKey{}, copied)
}

func allowedProvidersFrom(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	providers, _ := ctx.Value(allowedProvidersKey{}).([]string)
	return providers
}

type regionFilterKey struct{}

func WithRegionFilter(ctx context.Context, region string) context.Context {
	if strings.TrimSpace(region) == "" {
		return ctx
	}
	return context.WithValue(ctx, regionFilterKey{}, region)
}

func regionFilterFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	region, _ := ctx.Value(regionFilterKey{}).(string)
	return region
}

func requestContextDone(ctx context.Context, err error) bool {
	if ctx == nil || ctx.Err() == nil || err == nil {
		return false
	}
	// The caller's request has to be finished, and the error has to be that
	// context's own sentinel. errors.Is would also match an upstream that
	// wrapped context.Canceled into its own failure; that is an upstream
	// problem and must still fail over.
	return err == context.Canceled || err == context.DeadlineExceeded || errors.Is(err, ctx.Err())
}

const (
	routingPool         = "pool"
	routingPin          = "pin"
	routingSticky       = "sticky"
	routingStickyEscape = "sticky_escape"
)

// maxWorkerResponseBytes caps a fully buffered worker response. Non-stream
// completions are decoded from a single []byte; without a limit one inflated
// upstream payload could pin unbounded memory per in-flight request. 64 MiB
// matches translate.MaxCollectedResponseBytes used by the other collectors.
const maxWorkerResponseBytes = 64 << 20

type routingPlan struct {
	Source       string
	SessionKey   string
	BoundAccount string
	PublicModel  string
	// Aggregate marks a request routed through a logical (virtual) model.
	// Escaping such a request changes the concrete upstream model, so the
	// session must follow the member that answered rather than snap back to
	// a member that is merely cooling.
	Aggregate bool
}

func (e ChatExecutor) bindSession(plan routingPlan, accountID string) {
	if plan.Source == routingPin || plan.SessionKey == "" || e.SessionAffinity == nil {
		return
	}
	if plan.Source == routingStickyEscape && e.keepAnchorOnEscape(plan, accountID) {
		return
	}
	e.SessionAffinity.Bind(plan.SessionKey, accountID)
}

// keepAnchorMaxCooldown is how long a bound account may stay unavailable
// before an escape stops preserving the anchor. Upstream prompt caches are
// measured in minutes: below this window the original account's prefix is
// still warm and returning to it is cheaper than re-anchoring, above it the
// cache is gone and the chat should follow the account that actually serves.
const keepAnchorMaxCooldown = 5 * time.Minute

// keepAnchorOnEscape reports whether a sticky-escape should leave the existing
// binding intact. Keeping it lets the next turn return to the original account
// — and its warm prompt cache — as soon as a short cooldown lifts. A binding
// that would be wrong to keep:
//   - the account is gone, disabled, or the request is aggregated (escaping
//     swaps the concrete model, so the session must follow the answerer);
//   - the cooldown is long enough that the cached prefix has expired anyway.
func (e ChatExecutor) keepAnchorOnEscape(plan routingPlan, accountID string) bool {
	if e.SessionAffinity == nil || e.Pool == nil || plan.Aggregate {
		return false
	}
	anchor, ok := e.SessionAffinity.Peek(plan.SessionKey)
	if !ok || anchor == "" || anchor == accountID {
		return false
	}
	item, ok := e.Pool.ByID(anchor)
	if !ok {
		return false
	}
	if item.Ready != nil && !*item.Ready {
		return false
	}
	// Only a short, known cooldown keeps the anchor: the original account's
	// prompt cache is still warm, so returning there is cheaper than
	// re-anchoring. An unknown window (concurrency saturation has no resume
	// time) or a long quota wait means the cached prefix is gone and the chat
	// should follow the account that actually serves it.
	remaining := e.Pool.RetryAfter(item, plan.PublicModel)
	if remaining <= 0 || remaining > keepAnchorMaxCooldown {
		return false
	}
	return true
}

func (e ChatExecutor) observeRouting(plan *routingPlan, accountID string) {
	if plan == nil || plan.Source != routingSticky || plan.BoundAccount == "" {
		return
	}
	if strings.TrimSpace(accountID) == plan.BoundAccount {
		return
	}
	plan.Source = routingStickyEscape
	if e.SessionAffinity != nil {
		e.SessionAffinity.RecordEscape(e.stickyEscapeReason(plan))
	}
}

func (e ChatExecutor) stickyEscapeReason(plan *routingPlan) string {
	if plan == nil || e.Pool == nil {
		return "upstream_failover"
	}
	item, ok := e.Pool.ByID(plan.BoundAccount)
	if !ok {
		return "account_missing"
	}
	if item.Ready != nil && !*item.Ready {
		return "not_ready"
	}
	if !item.DownUntil.IsZero() && time.Now().Before(item.DownUntil) {
		return "account_cooldown"
	}
	model := accounts.CanonicalModelID(plan.PublicModel)
	if model != "auto" {
		if until, ok := item.ModelDownUntil[model]; ok && time.Now().Before(until) {
			return "model_cooldown"
		}
	}
	if item.MaxInFlight > 0 && item.InFlight >= item.MaxInFlight {
		return "concurrency_saturated"
	}
	return "upstream_failover"
}

// CommitSession binds a successfully completed stream. The executor cannot know
// whether an SSE response reached [DONE], so the relay calls this only after it
// has finished without an upstream or client error.
func (e ChatExecutor) CommitSession(ctx context.Context, req translate.ChatRequest, routing, accountID string) {
	if routing == routingPin || e.SessionAffinity == nil {
		return
	}
	key := resolveSessionKey(ctx, req)
	plan := routingPlan{Source: routing, SessionKey: key, PublicModel: req.Model}
	if routing == routingStickyEscape && e.keepAnchorOnEscape(plan, accountID) {
		return
	}
	e.SessionAffinity.Bind(key, accountID)
}

func itemProvider(item Item) string {
	return accounts.NormalizeProviderFamily(item.Provider)
}

// stickyAccountCanServeModel keeps a bound cooling empty-catalog account on
// the same model so regional escape still works, but does not let that
// unknown catalog pin a later, different model (Devin → Deepseek compact).
func stickyAccountCanServeModel(item Item, publicModel string) bool {
	if item.Models != nil {
		return ItemCouldServeModel(item, publicModel)
	}
	if strings.TrimSpace(publicModel) == "" {
		return true
	}
	if len(item.ProvenModels) == 0 {
		return true
	}
	probe := item
	probe.Models = []string{}
	return ItemCouldServeModel(probe, publicModel)
}

func (e ChatExecutor) prepareRouting(ctx context.Context, prefer, providerFilter string, req translate.ChatRequest, plan *AggregatePlan) (string, string, string, routingPlan) {
	prefer = strings.TrimSpace(prefer)
	providerFilter = strings.ToLower(strings.TrimSpace(providerFilter))
	publicModel := req.Model
	if prefer != "" {
		return prefer, providerFilter, "", routingPlan{Source: routingPin, PublicModel: publicModel, Aggregate: plan != nil}
	}

	planRoute := routingPlan{Source: routingPool, SessionKey: resolveSessionKey(ctx, req), PublicModel: publicModel, Aggregate: plan != nil}
	if planRoute.SessionKey == "" || e.SessionAffinity == nil || e.Pool == nil {
		return "", providerFilter, "", planRoute
	}
	accountID, ok := e.SessionAffinity.Get(planRoute.SessionKey)
	if !ok {
		return "", providerFilter, "", planRoute
	}
	item, ok := e.Pool.ByID(accountID)
	if !ok {
		e.SessionAffinity.RecordEscape("account_missing")
		e.SessionAffinity.Forget(planRoute.SessionKey)
		return "", providerFilter, "", planRoute
	}
	if providerFilter != "" && providerFilter != itemProvider(item) {
		e.SessionAffinity.RecordEscape("provider_mismatch")
		return "", providerFilter, "", planRoute
	}
	if !accounts.ProviderRegionAllowed(itemProvider(item), accounts.NormalizeRegion(item.Region), allowedProvidersFrom(ctx)) {
		e.SessionAffinity.RecordEscape("provider_not_allowed")
		return "", providerFilter, "", planRoute
	}
	if plan != nil {
		if !plan.CanServeAccount(item) {
			e.SessionAffinity.RecordEscape("model_unavailable")
			return "", providerFilter, "", planRoute
		}
	} else if !stickyAccountCanServeModel(item, publicModel) {
		e.SessionAffinity.RecordEscape("model_unavailable")
		return "", providerFilter, "", planRoute
	}
	return item.ID, itemProvider(item), accounts.NormalizeRegion(item.Region), routingPlan{
		Source: routingSticky, SessionKey: planRoute.SessionKey, BoundAccount: item.ID, PublicModel: publicModel,
		Aggregate: plan != nil,
	}
}

func NewChatExecutor(pool *Pool, workerKey string) ChatExecutor {
	if pool == nil {
		pool = NewPool(nil, nil)
	}
	affinityTTL, affinityCapacity := ResolveSessionAffinityConfig()
	return ChatExecutor{
		Pool:            pool,
		WorkerKey:       strings.TrimSpace(workerKey),
		MaxAttempts:     4,
		SessionAffinity: NewSessionAffinity(affinityTTL, affinityCapacity),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// capacityError reports the pick result for a route whose accounts all sit
// at their concurrency cap. Cooldown and quota results are not this error:
// those stay pinned so failover does not change for a provider that is
// waiting rather than full.
func capacityError(err error) bool {
	var execution *ExecutionError
	if !errors.As(err, &execution) {
		return false
	}
	return execution.Classified.Code == "rate_limit" && execution.Classified.Message == "all accounts at capacity"
}

// providerHasCapacity reports whether any provider other than the pinned one
// can take this model right now. The caller's own provider filter is a
// constraint, so saturation inside it is not a reason to leave.
func (e ChatExecutor) providerHasCapacity(pinnedProvider, pinnedRegion, publicModel string, allowed []string, eligible func(Item) bool) bool {
	if e.Pool == nil || pinnedProvider == "" {
		return false
	}
	query := e.routeQuery("", "", "", publicModel, allowed, nil, func(item Item) bool {
		if itemProvider(item) == pinnedProvider && (pinnedRegion == "" || accounts.NormalizeRegion(item.Region) == pinnedRegion) {
			return false
		}
		// A cooling account is not a free slot. HasCapacity also rejects one,
		// but only after routeMatches, and a model-scoped cooldown would
		// otherwise look like capacity and make a full provider leave for an
		// account the next pick cannot dispatch.
		now := time.Now()
		if itemDown(item, now) || itemModelDown(item, RouteQuery{PublicModel: publicModel}, now) {
			return false
		}
		return eligible == nil || eligible(item)
	})
	return e.Pool.HasCapacity(query)
}

func (e ChatExecutor) routeQuery(prefer, providerFilter, regionFilter, publicModel string, allowed []string, excluded map[string]struct{}, eligible func(Item) bool, plan ...*AggregatePlan) RouteQuery {
	q := RouteQuery{
		PublicModel:      publicModel,
		PreferAccount:    prefer,
		ProviderFilter:   providerFilter,
		RegionFilter:     regionFilter,
		AllowedProviders: allowed,
		Excluded:         excluded,
		Eligible:         eligible,
	}
	if len(plan) > 0 && plan[0] != nil {
		p := plan[0]
		q.ModelResolver = p.resolver()
		q.StrategyOverride = string(p.Strategy)
		if p.Strategy == accounts.AggregationPriority {
			q.PriorityResolver = p.priorityResolver()
		}
		q.WeightResolver = p.weightResolver()
	}
	return q
}

func (e ChatExecutor) pick(requestID, prefer, providerFilter, regionFilter, publicModel string, allowed []string, excluded map[string]struct{}, eligible func(Item) bool, plan *AggregatePlan) (Item, error) {
	query := e.routeQuery(prefer, providerFilter, regionFilter, publicModel, allowed, excluded, eligible, plan)
	if e.Pool != nil {
		if item, ok := e.Pool.PickRoute(query); ok {
			if retryAfter := e.Pool.RetryAfter(item, publicModel, query.ModelResolver); retryAfter > 0 {
				return Item{}, coolingPickError(item, publicModel, retryAfter)
			}
			return item, nil
		}
		// PickRoute returned false with an eligible route: every
		// eligible account is concurrency-saturated (a cooling account
		// would have been surfaced with ok=true for a retry-after hint).
		// Sending the request would just round-trip into the worker's
		// 429 busy, so fail fast with a rate-limit so the client gets a
		// clean Retry-After. This must precede the model_not_available
		// check: saturated means the model IS served, just at capacity.
		if e.Pool.LenRoute(query) > 0 {
			return Item{}, NewExecutionError(Classified{
				Kind: accounts.KindRateLimit, Status: 429, Code: "rate_limit",
				Type: "api_error", Message: "all accounts at capacity",
				Cooldown: 5 * time.Second, RetryAfter: 5 * time.Second, Failover: true,
			}, nil)
		}
		// No eligible account is ready right now. Before blaming the model or
		// reporting "no accounts available", check whether accounts that could
		// serve this model are blocked only by an exhausted quota. A stale or
		// genuinely exhausted snapshot leaves no routing hint, so without this
		// the client sees a generic "no accounts available" instead of a quota
		// error that explains the real cause.
		if _, ok := e.Pool.QuotaBlockedForModel(query); ok {
			retryAfter := NextLocalMidnightCooldown()
			return Item{}, NewExecutionError(Classified{
				Kind: accounts.KindQuota, Status: 429, Code: "insufficient_quota",
				Type: "insufficient_quota", Failover: false,
				Cooldown: retryAfter, RetryAfter: retryAfter,
				Message: quotaBlockedMessage(publicModel),
			}, nil)
		}
		if publicModel != "" && publicModel != "auto" {
			unfiltered := query
			unfiltered.PublicModel = ""
			if e.Pool.LenRoute(unfiltered) > 0 {
				e.logModelRouteMiss(requestID, query)
				return Item{}, NewExecutionError(Classified{
					Kind: accounts.KindModelNotAvailable, Status: http.StatusBadRequest,
					Code: "model_not_available", Type: "invalid_request_error", Failover: false,
					Message: fmt.Sprintf("model_not_available: %s is not available for the selected accounts", publicModel),
				}, nil)
			}
		}
	}
	if len(allowed) > 0 && providerFilter != "" && !accounts.ProviderAllowed(providerFilter, allowed) {
		return Item{}, NewExecutionError(Classified{
			Kind: accounts.KindAuth, Status: http.StatusForbidden, Code: "provider_not_allowed",
			Type: "permission_error", Failover: false,
			Message: fmt.Sprintf("api key cannot use provider %s", providerFilter),
		}, nil)
	}
	if providerFilter != "" && regionFilter != "" {
		return Item{}, noAccountsError(fmt.Sprintf("no %s/%s accounts available", providerFilter, regionFilter))
	}
	if providerFilter != "" {
		// A key whose grant list narrows this family to a single region
		// should fail with that region in the message, even when the
		// request itself did not pin one.
		if regionFilter, narrowed := keyGrantedSingleRegion(providerFilter, allowed); narrowed {
			return Item{}, noAccountsError(fmt.Sprintf("no %s/%s accounts available", providerFilter, regionFilter))
		}
		return Item{}, noAccountsError(fmt.Sprintf("no %s accounts available", providerFilter))
	}
	if len(allowed) > 0 {
		return Item{}, noAccountsError("no accounts available for this api key")
	}
	return Item{}, NewExecutionError(Classified{
		Kind: accounts.KindUnavailable, Status: http.StatusServiceUnavailable,
		Code: "no_accounts_configured", Type: "api_error", Failover: false,
		Message: "no worker accounts configured",
	}, nil)
}

// noAccountsRetryAfter mirrors runtime's workerReconcileInterval: the liveness
// watchdog re-probes an unready worker at most once per this interval, so a
// retry before then cannot observe a recovery and would just repeat the miss.
const noAccountsRetryAfter = 2 * time.Minute

// noAccountsError reports that routing found no account able to serve this
// key/model. It must be a classified error: a bare error reaches the gateway as
// an unclassified failure, which renders it as 502 "upstream_error". That reads
// as an origin outage to every client and to any proxy in front of the gateway
// (Cloudflare replaced it with its own "Bad gateway" page), while the real
// cause is local routing — no enabled account matches the key's provider grant
// or the requested model. 503 matches KindUnavailable; 429 is reserved for
// rate/credit limits in this taxonomy, and SDKs auto-retry it on a fixed
// schedule even though every such retry fails until the account is restored.
// The Retry-After hint lines up with the watchdog's per-account re-probe
// cadence, which is when a self-healing miss can actually clear.
func noAccountsError(message string) error {
	return NewExecutionError(Classified{
		Kind: accounts.KindUnavailable, Status: http.StatusServiceUnavailable,
		Code: "no_accounts", Type: "api_error", Message: message,
		Cooldown: noAccountsRetryAfter, RetryAfter: noAccountsRetryAfter, Failover: false,
	}, nil)
}

func (e ChatExecutor) logModelRouteMiss(requestID string, query RouteQuery) {
	if e.Pool == nil {
		return
	}
	allowed := append([]string(nil), query.AllowedProviders...)
	sort.Strings(allowed)
	excluded := make([]string, 0, len(query.Excluded))
	for id := range query.Excluded {
		excluded = append(excluded, id)
	}
	sort.Strings(excluded)
	summaries := make([]string, 0, e.Pool.Len())
	for _, item := range e.Pool.Items() {
		summaries = append(summaries, modelRouteAccountSummary(item, query.PublicModel, query.Excluded))
	}
	log.Printf("model route unavailable request_id=%q model=%q provider=%q region=%q prefer=%q allowed=%q excluded=%q accounts=[%s]",
		requestID, query.PublicModel, query.ProviderFilter, query.RegionFilter, query.PreferAccount,
		strings.Join(allowed, ","), strings.Join(excluded, ","), strings.Join(summaries, " "))
}

func modelRouteAccountSummary(item Item, publicModel string, excluded map[string]struct{}) string {
	ready := item.Ready == nil || *item.Ready
	hot := item.Hot != nil && *item.Hot
	quotaExceeded := item.Quota != nil && item.Quota.Exceeded
	_, isExcluded := excluded[item.ID]
	want := accounts.CanonicalModelID(publicModel)
	catalogHas, provenHas := false, false
	for _, model := range item.Models {
		catalogHas = catalogHas || accounts.CanonicalModelID(model) == want
	}
	for _, model := range item.ProvenModels {
		provenHas = provenHas || accounts.CanonicalModelID(model) == want
	}
	catalog := "unknown"
	if item.Models != nil {
		catalog = fmt.Sprintf("count:%d models:%s", len(item.Models), compactModelList(item.Models, 12))
	}
	catalogAge := "unknown"
	if !item.ModelsAt.IsZero() {
		catalogAge = time.Since(item.ModelsAt).Round(time.Second).String()
	}
	modelDownUntil := time.Time{}
	if item.ModelDownUntil != nil {
		modelDownUntil = item.ModelDownUntil[want]
	}
	return fmt.Sprintf("{id:%q provider:%q region:%q ready:%t hot:%t quota_exceeded:%t down_until:%q model_down_until:%q in_flight:%d/%d excluded:%t catalog_has:%t proven_has:%t catalog_age:%q catalog:%q}",
		item.ID, item.Provider, item.Region, ready, hot, quotaExceeded, logTime(item.DownUntil), logTime(modelDownUntil),
		item.InFlight, item.MaxInFlight, isExcluded, catalogHas, provenHas, catalogAge, catalog)
}

func compactModelList(models []string, limit int) string {
	if len(models) == 0 {
		return "[]"
	}
	if limit <= 0 || limit > len(models) {
		limit = len(models)
	}
	shown := append([]string(nil), models[:limit]...)
	if limit < len(models) {
		return fmt.Sprintf("[%s,+%d]", strings.Join(shown, ","), len(models)-limit)
	}
	return "[" + strings.Join(shown, ",") + "]"
}

func logTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func quotaBlockedMessage(publicModel string) string {
	if strings.TrimSpace(publicModel) != "" {
		return fmt.Sprintf("all accounts that can serve %s are out of quota", publicModel)
	}
	return "all accounts are out of quota"
}

func coolingPickError(item Item, publicModel string, retryAfter time.Duration) error {
	failover := true
	kind := accounts.KindRateLimit
	code := "rate_limit"
	typ := "api_error"
	message := "all accounts are cooling down"
	if !item.DownUntil.IsZero() && time.Now().Before(item.DownUntil) && item.LastKind == accounts.KindQuota {
		kind = accounts.KindQuota
		code = "insufficient_quota"
		typ = "insufficient_quota"
		// The pool is already out of accounts, so this response is terminal
		// even though a single account's quota error would fail over.
		if strings.TrimSpace(publicModel) != "" {
			message = fmt.Sprintf("all accounts that can serve %s are on quota cooldown", publicModel)
		} else {
			message = "all accounts are on quota cooldown"
		}
	} else if strings.TrimSpace(publicModel) != "" {
		if until, ok := item.ModelDownUntil[accounts.CanonicalModelID(publicModel)]; ok && !until.IsZero() {
			message = fmt.Sprintf("model %s is cooling down on all available accounts", publicModel)
		}
	}
	return NewExecutionError(Classified{
		Kind: kind, Status: 429, Code: code, Type: typ, Message: message,
		Cooldown: retryAfter, RetryAfter: retryAfter, Failover: failover,
	}, nil)
}

func (e ChatExecutor) attemptsFor(providerFilter, regionFilter, publicModel string, allowed []string, predicate func(Item) bool, resolver ...func(Item) string) int {
	var resolve func(Item) string
	if len(resolver) > 0 {
		resolve = resolver[0]
	}
	maxAttempts := e.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 4
	}
	if maxAttempts > 64 {
		maxAttempts = 64
	}
	if e.Pool != nil {
		query := e.routeQuery("", providerFilter, regionFilter, publicModel, allowed, nil, predicate)
		query.ModelResolver = resolve
		if n := e.Pool.LenRoute(query); n > 0 {
			if n > maxAttempts {
				return maxAttempts
			}
			return n
		}
	}
	return 1
}

func pinRegion(current, next string) string {
	if current != "" {
		return current
	}
	return accounts.NormalizeRegion(next)
}

// keyGrantedSingleRegion inspects an API key allowlist for one provider
// family. It returns the single region the grants narrow the family to, with
// narrowed=true. A bare family entry or an empty allowlist means every region
// (narrowed=false); two or more distinct region grants also stay false — in
// that case region stickiness keeps working exactly as before: the first
// picked account decides the region for the rest of the request.
func keyGrantedSingleRegion(providerFilter string, allowed []string) (string, bool) {
	if providerFilter == "" || len(allowed) == 0 {
		return "", false
	}
	regions, allRegions := accounts.GrantedRegions(providerFilter, allowed)
	if allRegions || len(regions) != 1 {
		return "", false
	}
	return regions[0], true
}

// qoderDirect reports whether this Qoder account chats through the registered
// adapter instead of its login worker. Tests that stub a worker URL and do not
// register the adapter stay on the worker path.
func (e ChatExecutor) qoderDirect(item Item) bool {
	if itemProvider(item) != "qoder" || e.Providers == nil {
		return false
	}
	adapter, ok := e.Providers.Get("qoder")
	return ok && adapter.Chat != nil
}

func isInProcessItem(item Item) bool {
	if item.Runtime == string(providers.RuntimeInProcess) {
		return true
	}
	if itemProvider(item) != "qoder" && item.URL == "" {
		return true
	}
	return false
}

// ObserveStreamFailure applies a post-headers streaming failure to the pool.
// Upstream can answer 200 and then fail inside the SSE body, which happens
// after the executor already returned a successful StreamResult; the relay
// error is the first place the failure is observable. Same-account retry is
// impossible at that point (bytes are on the wire), so this only records the
// classified state for the next request's scheduling.
func (e ChatExecutor) ObserveStreamFailure(accountID string, err error, model string) {
	if e.Pool == nil || accountID == "" || err == nil || requestContextDone(nil, err) {
		return
	}
	classified := e.classifyInProcessError(err)
	if classified.Kind == "" || classified.Kind == accounts.KindCanceled {
		return
	}
	if classified.Kind == accounts.KindInvalidRequest {
		// The request body was rejected; the account itself is healthy.
		return
	}
	if classified.Kind == accounts.KindModelNotAvailable {
		e.handleModelAvailabilityFailure("", "stream_body", accountID, model, classified)
		return
	}
	if classified.Kind == accounts.KindQuota && classified.Cooldown <= 0 {
		// The response already went out with 200, so there is no other account
		// to fail over to. Still cool this one down, or the next request picks
		// it again and fails the same way.
		classified.Cooldown = NextLocalMidnightCooldown()
	}
	e.markClassified(accountID, classified, model)
}

// errEmptyStream means the upstream answered 200 and then closed the body
// before sending a single SSE line. Historically quota-exhausted accounts did
// this and the executor recorded it as a success.
var errEmptyStream = errors.New("upstream answered 200 but the stream was empty before the first event")

// replayReadCloser replays the bytes consumed by the probe and then continues
// reading from the live upstream body.
type replayReadCloser struct {
	io.Reader
	io.Closer
}

// probeStreamBody waits for the first event of an upstream stream. Nothing has
// been sent to the client yet at this point, so a stream that dies or reports
// an error before the first event can still fail over to another account.
// Blank lines and comments (: keep-alive) are skipped; the bytes read while
// probing are replayed so the relay sees the stream from the first byte.
// The returned Classified is only meaningful when err != nil. A read that
// fails because the caller's request was canceled returns that context error,
// so the caller can report KindCanceled instead of treating the account as
// healthy. A body that merely wraps a context error while the request is still
// alive is an upstream failure and fails over.
func probeStreamBody(ctx context.Context, body io.ReadCloser) (io.ReadCloser, Classified, error) {
	if body == nil {
		return nil, Classify(0, errEmptyStream.Error(), "", accounts.KindUnavailable, ""), errEmptyStream
	}
	reader := bufio.NewReaderSize(body, 64*1024)
	var consumed bytes.Buffer
	eventName := ""
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			consumed.WriteString(line)
			text := strings.TrimSpace(strings.TrimRight(line, "\r\n"))
			switch {
			case text == "", strings.HasPrefix(text, ":"):
				// Frame separator or keep-alive comment: keep waiting.
			case strings.HasPrefix(text, "event:"):
				eventName = strings.TrimSpace(strings.TrimPrefix(text, "event:"))
			case strings.HasPrefix(text, "data:"):
				data := strings.TrimSpace(strings.TrimPrefix(text, "data:"))
				if classified, failed := probeFrameFailure(eventName, data); failed {
					body.Close()
					return nil, classified, fmt.Errorf("upstream stream failed before the first event: %s", classified.Message)
				}
				return &replayReadCloser{Reader: io.MultiReader(&consumed, reader), Closer: body}, Classified{}, nil
			default:
				// Non-SSE payload: let the relay surface it unchanged.
				return &replayReadCloser{Reader: io.MultiReader(&consumed, reader), Closer: body}, Classified{}, nil
			}
		}
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				// A context error is not an account failure. Hand back what was
				// already read and let the relay decide: a canceled request is
				// reported as a cancellation, and a body that wrapped
				// context.Canceled while the request is still alive replays
				// through the relay, which fails over without a pre-stream 502.
				return &replayReadCloser{Reader: io.MultiReader(&consumed, reader), Closer: body}, Classified{}, nil
			}
			body.Close()
			if errors.Is(err, io.EOF) {
				return nil, Classify(0, errEmptyStream.Error(), "", accounts.KindUnavailable, ""), errEmptyStream
			}
			return nil, Classify(0, err.Error(), "", accounts.KindUnavailable, ""), err
		}
	}
}

// probeFrameFailure recognizes an SSE error frame, such as the structured
// "event: error" the worker emits when the upstream dies before streaming any
// token. Content frames (the ones carrying choices) are not failures.
func probeFrameFailure(eventName, data string) (Classified, bool) {
	if data == "" {
		return Classified{}, false
	}
	var payload map[string]any
	if json.Unmarshal([]byte(data), &payload) != nil {
		return Classified{}, false
	}
	if _, ok := payload["choices"]; ok {
		return Classified{}, false
	}
	status := 0
	if value, ok := payload["status"].(float64); ok {
		status = int(value)
	}
	if _, ok := payload["error"]; ok {
		return Classify(status, data, "", "", ""), true
	}
	if strings.EqualFold(eventName, "error") {
		return Classify(status, data, "", "", ""), true
	}
	return Classified{}, false
}

func (e ChatExecutor) handleModelAvailabilityFailure(requestID, source, accountID, model string, classified Classified) {
	if e.Pool == nil || accountID == "" {
		return
	}
	item, _ := e.Pool.ByID(accountID)
	action := "preserve_catalog"
	if shouldEvictUnavailableModel(classified) {
		e.Pool.RemoveModel(accountID, model)
		action = "evict_model"
	}
	log.Printf("model route account failure request_id=%q source=%q account=%q provider=%q region=%q model=%q kind=%q code=%q status=%d action=%q message=%q account_state=%s",
		requestID, source, accountID, item.Provider, item.Region, model, classified.Kind, classified.Code,
		classified.Status, action, truncateLogValue(classified.Message, 300), modelRouteAccountSummary(item, model, nil))
}

func shouldEvictUnavailableModel(classified Classified) bool {
	if classified.Kind != accounts.KindModelNotAvailable {
		return false
	}
	searchable := strings.ToLower(strings.Join([]string{classified.Code, classified.Message}, " "))
	return !strings.Contains(searchable, "model_catalog_unavailable") &&
		!strings.Contains(searchable, "dynamic model catalog is unavailable") &&
		!strings.Contains(searchable, "model catalog unavailable")
}

func truncateLogValue(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

// markClassified records a classified failure. model scopes the cooldown to
// the requested public model so one rate-limited model does not take the
// whole account offline; pass "" for an account-wide cooldown.
func (e ChatExecutor) markClassified(id string, c Classified, model string) {
	if e.Pool == nil || id == "" {
		return
	}
	if c.Model == "" && c.Kind == accounts.KindRateLimit {
		c.Model = e.cooldownModel(id, model)
	}
	e.Pool.MarkClassified(id, c)
}

// markOK records a success scoped to the requested public model so a 200
// on one model does not discard a cooldown recorded for another. Pass an
// empty model for a true account-level recovery.
func (e ChatExecutor) markOK(id, model string) {
	if e.Pool == nil {
		return
	}
	e.Pool.MarkOK(id, e.cooldownModel(id, model))
}

// cooldownModel records the account's real catalog model when the client
// used that account's alias, so the alias and the real name share one cooldown.
func (e ChatExecutor) cooldownModel(id, model string) string {
	if e.Pool == nil || id == "" {
		return model
	}
	item, ok := e.Pool.ByID(id)
	if !ok {
		return model
	}
	return AccountModelID(item, model)
}

func (e ChatExecutor) recordAttempt(ctx context.Context, attempt accounts.RequestAttempt) {
	if e.OnAttempt == nil {
		return
	}
	if attempt.ID == "" {
		attempt.ID = accounts.NewAttemptID()
	}
	if attempt.RequestID == "" {
		attempt.RequestID = RequestIDFromContext(ctx)
	}
	if attempt.RequestID == "" {
		return
	}
	e.OnAttempt(attempt)
}

// buildWorkerChatPayload is the OpenAI-shaped body a non-Qoder worker stub
// still accepts. Qoder production chat does not use it.
func buildWorkerChatPayload(req translate.ChatRequest, stream bool) map[string]any {
	payload := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   stream,
	}
	if len(req.MaxCompletionTokens) > 0 {
		payload["max_tokens"] = json.RawMessage(req.MaxCompletionTokens)
	} else if len(req.MaxTokens) > 0 {
		payload["max_tokens"] = json.RawMessage(req.MaxTokens)
	}
	if len(req.Temperature) > 0 {
		payload["temperature"] = json.RawMessage(req.Temperature)
	}
	if len(req.Tools) > 0 {
		payload["tools"] = json.RawMessage(req.Tools)
	}
	if len(req.ToolChoice) > 0 {
		payload["tool_choice"] = json.RawMessage(req.ToolChoice)
	}
	if len(req.ReasoningEffort) > 0 {
		payload["reasoning_effort"] = json.RawMessage(req.ReasoningEffort)
	}
	return payload
}

func (e ChatExecutor) newWorkerRequest(ctx context.Context, item Item, payload []byte, prefer string) (*http.Request, error) {
	account := prefer
	if account == "" {
		account = item.ID
	}
	key := e.WorkerKey
	if e.WorkerKeySource != nil {
		key = e.WorkerKeySource()
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(item.URL, "/")+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if key != "" {
		httpReq.Header.Set("Authorization", "Bearer "+key)
	}
	if account != "" {
		httpReq.Header.Set("X-Qoder-Account", account)
	}
	if requestID := RequestIDFromContext(ctx); requestID != "" {
		httpReq.Header.Set("X-Request-Id", requestID)
	}
	return httpReq, nil
}

func classifyWorkerErr(resp *http.Response, body string) Classified {
	status := 0
	retryAfter := ""
	kind := ""
	failover := ""
	if resp != nil {
		status = resp.StatusCode
		retryAfter = resp.Header.Get("Retry-After")
		kind = resp.Header.Get("X-Qoder-Error-Kind")
		failover = resp.Header.Get("X-Qoder-Failover")
	}
	return Classify(status, body, retryAfter, kind, failover)
}

type routeLoop struct {
	requestID      string
	prefer         string
	providerFilter string
	regionFilter   string
	routing        routingPlan
	excluded       map[string]struct{}
	lastErr        error
	allowed        []string
	attempts       int
	pinned         string
	index          int
	// regionPinned records that this request chose its region implicitly from
	// the first picked account (not from a model prefix, grant, or explicit
	// pin). A later failure releases that pin so failover can cross regions.
	regionPinned bool
	// eligible narrows candidates to one protocol capability; nil admits all.
	eligible func(Item) bool
	// resolver maps an item to its concrete aggregated model. Nil for normal
	// (non-aggregated) requests. Pure and read-only; safe under Pool.mu.
	resolver func(Item) string
	// plan is the immutable per-request aggregate plan; nil for normal routes.
	// It carries the per-model strategy/weights used by Pool.PickRoute.
	plan *AggregatePlan
	// tierAttempts and tierBudget apply only to priority aggregation. Each
	// priority tier gets its own budget, so sibling accounts of the first
	// provider cannot consume every retry before a lower-priority provider is
	// reached. When a tier is exhausted its remaining accounts are added to
	// excluded.
	tierAttempts map[int]int
	tierBudget   int
	// sameAccountRetries counts how many transient same-account retries this
	// request has already spent. It is a separate budget from attempts: a
	// retry on the same account must not consume a failover hop, because
	// attemptsFor already caps attempts at the number of candidate accounts.
	sameAccountRetries int
}

// maxSameAccountRetries bounds same-account retries per request. One is enough:
// the transient cases are empty/heartbeat-only streams and a first-byte
// timeout, where a second identical attempt almost always succeeds, and
// more retries would stack latency ahead of a real failover.
const maxSameAccountRetries = 1

func (e ChatExecutor) newRouteLoop(ctx context.Context, prefer, providerFilter string, req translate.ChatRequest) routeLoop {
	aggPlan := aggregatePlanFrom(ctx)
	prefer, providerFilter, regionFilter, routing := e.prepareRouting(ctx, prefer, providerFilter, req, aggPlan)
	allowed := allowedProvidersFrom(ctx)
	// Capture the aggregate resolver once from the immutable per-request plan.
	// If a plan exists, the public model is virtual and must not be pinned by a
	// provider prefix; the plan's members already carry provider/region.
	var resolver func(Item) string
	if aggPlan != nil {
		resolver = aggPlan.resolver()
		providerFilter, regionFilter = "", ""
		switch routing.Source {
		case routingSticky:
			// Keep the cache-affine binding. The public ID is virtual, so the
			// account was already validated against the plan's members in
			// prepareRouting; prefer selects it without re-imposing a provider.
			prefer = routing.BoundAccount
		case routingPin:
			// An explicit pin is useful only when the pinned account can serve
			// a member of this logical model.
			if e.Pool != nil {
				if pinned, ok := e.Pool.ByID(prefer); ok && aggPlan.CanServeAccount(pinned) {
					break
				}
			}
			prefer = ""
			routing = routingPlan{Source: routingPool, SessionKey: routing.SessionKey, PublicModel: req.Model, Aggregate: true}
		default:
			prefer = ""
			routing = routingPlan{Source: routingPool, SessionKey: routing.SessionKey, PublicModel: req.Model, Aggregate: true}
		}
	}
	// A model-name region prefix (e.g. "workbuddy-cn/model") sets an explicit
	// region that overrides sticky-session and pin-derived regions. It
	// coexists with key grants: routeBaseMatches enforces both filters
	// independently, so a prefix that conflicts with a grant yields no
	// candidates and a clean error.
	if modelRegion := regionFilterFromContext(ctx); modelRegion != "" {
		regionFilter = modelRegion
	} else if providerFilter != "" {
		// A key whose grants narrow the selected family to exactly one region
		// pre-seeds the region filter: scheduling never even considers accounts
		// of other regions — including a pin to an out-of-grant account (the
		// grant overrides the pinned account's region, so the pin falls back
		// into the granted region) and every failover hop.
		if granted, narrowed := keyGrantedSingleRegion(providerFilter, allowed); narrowed {
			regionFilter = granted
		}
	}
	if aggPlan == nil && regionFilter == "" && prefer != "" && e.Pool != nil {
		if pinnedItem, ok := e.Pool.ByID(prefer); ok {
			regionFilter = pinRegion("", pinnedItem.Region)
		}
	}
	// Per-model failover settings win over the executor global attempt budget.
	// AllowFallbacks=false means the single best account only; MaxAttempts caps
	// how many distinct accounts/members may be tried for one logical model.
	//
	// Keep aggregated routes at the same single candidate scan as normal routes:
	// attemptsFor already does that scan, so a plan must not call attemptsFor and
	// then LenRoute again. Only count candidates here when failover is possible.
	var attempts int
	var tierBudget int
	if aggPlan != nil {
		attempts = aggPlan.MaxAttempts
		if attempts <= 0 {
			attempts = 1
		}
		if !aggPlan.AllowFallbacks {
			attempts = 1
		} else if aggPlan.Strategy == accounts.AggregationPriority {
			// LiteLLM-style ordered fallback: each order tier gets its own
			// retry budget before escalating to the next tier. A global budget
			// would otherwise be spent entirely on sibling accounts of the
			// first provider when MaxAttempts is smaller than that provider's
			// account count.
			tierBudget = attempts
			attempts *= aggPlan.PriorityTierCount()
			if e.Pool != nil {
				query := e.routeQuery("", providerFilter, regionFilter, req.Model, allowed, nil, nil, aggPlan)
				if n := e.Pool.LenRoute(query); n > 0 && n < attempts {
					attempts = n
				}
			}
		} else if e.Pool != nil {
			query := e.routeQuery("", providerFilter, regionFilter, req.Model, allowed, nil, nil, aggPlan)
			if n := e.Pool.LenRoute(query); n > 0 && n < attempts {
				attempts = n
			}
		}
	} else {
		attempts = e.attemptsFor(providerFilter, regionFilter, req.Model, allowed, nil)
	}
	loop := routeLoop{
		requestID:      RequestIDFromContext(ctx),
		prefer:         prefer,
		providerFilter: providerFilter,
		regionFilter:   regionFilter,
		routing:        routing,
		excluded:       map[string]struct{}{},
		allowed:        allowed,
		resolver:       resolver,
		plan:           aggPlan,
		attempts:       attempts,
	}
	if tierBudget > 0 {
		loop.tierAttempts = make(map[int]int)
		loop.tierBudget = tierBudget
	}
	if routing.Source == routingPin {
		loop.pinned = prefer
	}
	return loop
}

func (l *routeLoop) pickNext(e ChatExecutor, publicModel string) (Item, int, error) {
	item, err := e.pick(l.requestID, l.prefer, l.providerFilter, l.regionFilter, publicModel, l.allowed, l.excluded, l.eligible, l.plan)
	if err != nil {
		// Sticky routing pins the bound provider and region before this pick.
		// A capacity check made before the pick races the slot: another request
		// can fill the last one in between, and the pin then turns a full
		// provider into rate_limit while another provider still has room.
		// Drop the pin only after the pick itself reports saturation, and only
		// when this request did not ask for that provider.
		if l.routing.Source == routingSticky && l.index == 0 && l.pinned == "" && capacityError(err) && e.providerHasCapacity(l.providerFilter, l.regionFilter, publicModel, l.allowed, l.eligible) {
			if e.SessionAffinity != nil {
				e.SessionAffinity.RecordEscape("concurrency_saturated")
			}
			l.routing.Source = routingPool
			l.prefer = ""
			l.providerFilter = ""
			l.regionFilter = ""
			l.regionPinned = false
			l.attempts = e.attemptsFor(l.providerFilter, l.regionFilter, publicModel, l.allowed, l.eligible, l.resolver)
			item, err = e.pick(l.requestID, l.prefer, l.providerFilter, l.regionFilter, publicModel, l.allowed, l.excluded, l.eligible, l.plan)
		}
		if err != nil {
			return Item{}, l.index, err
		}
	}
	attemptIndex := l.index
	e.observeRouting(&l.routing, item.ID)
	l.prefer = ""
	// Pin the first pick's region so a mixed-region pool does not flip regions
	// while rotating within one request. The attempt budget was computed over
	// every eligible candidate before this pick and must not shrink here:
	// shrinking it to the pinned region's account count can leave a failed
	// request with no failover hop at all, even though another provider or
	// region is healthy. A failure releases the pin in exclude/failover.
	if l.plan == nil && l.regionFilter == "" {
		l.regionFilter = pinRegion(l.regionFilter, item.Region)
		l.regionPinned = true
	}
	l.index++
	return item, attemptIndex, nil
}

// attemptModel returns the concrete model used for this item. It is the
// resolved real model for aggregated requests, otherwise the public model.
// Cooldowns must be recorded against this value so different providers/models
// behind one virtual ID do not share a cooldown.
func (l routeLoop) attemptModel(item Item, req translate.ChatRequest) string {
	if l.resolver != nil {
		if model := l.resolver(item); model != "" {
			return model
		}
	}
	return req.Model
}

// dispatchRequest returns the request for this attempt with an aggregated
// virtual model resolved to the concrete model chosen for this item. It only
// touches the local copy of the request; the shared Item is never modified.
func (l routeLoop) dispatchRequest(item Item, req translate.ChatRequest) translate.ChatRequest {
	req.Model = l.attemptModel(item, req)
	return req
}

func (l routeLoop) canFailover(classified Classified) bool {
	return classified.Failover && l.index < l.attempts
}

// canSameAccountRetry reports whether this failure may be retried once on the
// account that produced it. Only transient upstream hiccups qualify, and only
// while the caller is still connected: retrying after the client left would
// burn an upstream slot for nothing.
func (l *routeLoop) canSameAccountRetry(ctx context.Context, classified Classified) bool {
	if l.sameAccountRetries >= maxSameAccountRetries {
		return false
	}
	if !classified.Transient {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	return true
}

func (l *routeLoop) exclude(item Item) {
	l.excluded[item.ID] = struct{}{}
	// A cross-provider route (no explicit provider filter) must be able to
	// leave the region its first pick happened to choose; otherwise a bare
	// model shared by several providers stays pinned to one supplier.
	//
	// A provider-scoped route keeps its region pin: Qoder global and CN are
	// isolated upstreams (different catalogs/quota pools), and existing
	// behavior explicitly forbids global -> CN failover. Aggregated routes
	// carry their own provider/region per member and never pin here.
	if l.regionPinned && l.providerFilter == "" {
		l.regionFilter = ""
		l.regionPinned = false
	}
}

// failover records an upstream failure and excludes the account. On a
// cross-provider route exclude also releases the implicit region pin, so the
// next pick can leave the first supplier's region. Provider-scoped routes keep
// their region pin.
//
// The session binding survives a cooldown-carrying failure: the account is
// parked, not dead, and the next request should fall through to a sibling and
// then return to the original account (and its warm prompt cache). A failure
// that leaves no cooldown invalidates the binding instead, because retrying
// that account first next turn would fail identically.
func (l *routeLoop) failover(e ChatExecutor, item Item, classified Classified) {
	l.exclude(item)
	l.noteTierFailure(e, item)
	// Any non-pin route that already carries a session key can have a stale
	// binding (including a sticky-escape that started on another account).
	//
	// A cooling account is only temporarily unavailable. Keep the binding so
	// the next turn's PreferAccount falls through to a healthy sibling now and
	// returns to the original account — and its warm prompt cache — once the
	// cooldown lifts. Only a failure that leaves no cooldown (a cold upstream
	// or a model this account does not serve) invalidates the anchor, because
	// retrying it first next turn would just fail the same way.
	if l.routing.Source != routingPin && l.routing.SessionKey != "" && e.SessionAffinity != nil {
		if classified.Cooldown <= 0 {
			e.SessionAffinity.Forget(l.routing.SessionKey)
		}
	}
}

// noteTierFailure gives each priority tier its own retry budget. Once the
// budget is consumed, the remaining accounts in that tier are excluded so the
// next pick can reach a lower-priority provider instead of retrying every
// sibling account of the same provider.
func (l *routeLoop) noteTierFailure(e ChatExecutor, item Item) {
	if l.plan == nil || l.plan.Strategy != accounts.AggregationPriority || l.tierAttempts == nil || e.Pool == nil {
		return
	}
	tier, ok := l.plan.Priority(item)
	if !ok {
		return
	}
	l.tierAttempts[tier]++
	if l.tierAttempts[tier] < l.tierBudget {
		return
	}
	for _, candidate := range e.Pool.Items() {
		if candidateTier, ok := l.plan.Priority(candidate); ok && candidateTier == tier {
			l.excluded[candidate.ID] = struct{}{}
		}
	}
}

func (l routeLoop) headerAccount(item Item, attemptIndex int) string {
	if attemptIndex == 0 && l.pinned != "" {
		return l.pinned
	}
	return item.ID
}

func (l routeLoop) resultProvider(item Item) string {
	return firstNonEmpty(item.Provider, "qoder")
}

func (l routeLoop) pickFailure(err error) (int, string, string, error) {
	if l.lastErr != nil {
		return l.index, lastAccountID(l.excluded), l.providerFilter, l.lastErr
	}
	return 0, "", "", err
}

// releaseCloser runs release exactly once when the body is closed, so a
// successful stream keeps its Acquire until the caller finishes reading.
type releaseCloser struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (c *releaseCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.release != nil {
		c.once.Do(c.release)
	}
	return err
}

func (e ChatExecutor) acquireInFlight(requestID, id string) bool {
	if e.Pool == nil {
		if e.OnInFlightAcquire != nil && requestID != "" && id != "" {
			e.OnInFlightAcquire(requestID, id)
		}
		return true
	}
	if !e.Pool.Acquire(id) {
		return false
	}
	if e.OnInFlightAcquire != nil && requestID != "" {
		e.OnInFlightAcquire(requestID, id)
	}
	return true
}

func (e ChatExecutor) releaseInFlight(requestID, id string) {
	if e.Pool != nil {
		e.Pool.Release(id)
	}
	if e.OnInFlightRelease != nil && requestID != "" {
		e.OnInFlightRelease(requestID, id)
	}
}

// attachInFlightRelease transfers an acquired slot to the response body.
// Close releases the pool id that was Acquired (not a rewritten account id).
func (e ChatExecutor) attachInFlightRelease(resp *http.Response, requestID, poolID string) *http.Response {
	if resp == nil || resp.Body == nil {
		e.releaseInFlight(requestID, poolID)
		return resp
	}
	resp.Body = &releaseCloser{ReadCloser: resp.Body, release: func() { e.releaseInFlight(requestID, poolID) }}
	return resp
}

func (e ChatExecutor) ChatNonStream(ctx context.Context, req translate.ChatRequest, prefer, providerFilter string) (result ChatResult, returnErr error) {
	loop := e.newRouteLoop(ctx, prefer, providerFilter, req)
	defer func() { result.Routing = loop.routing.Source }()
	for loop.index < loop.attempts {
		item, i, err := loop.pickNext(e, req.Model)
		if err != nil {
			attempts, accountID, provider, pickErr := loop.pickFailure(err)
			if loop.lastErr != nil {
				return ChatResult{AttemptCount: attempts, AccountID: accountID, Provider: provider}, pickErr
			}
			return ChatResult{}, pickErr
		}
		// Acquire with the pool id before any rewrite of item.ID (X-Qoder-Account).
		poolID := item.ID
		if !e.acquireInFlight(loop.requestID, poolID) {
			loop.exclude(item)
			continue
		}
		release := func() { e.releaseInFlight(loop.requestID, poolID) }
		if isInProcessItem(item) || e.qoderDirect(item) {
			// Retry a transient upstream hiccup once on the same account before
			// spending a failover hop. The prompt cache is still warm here, so a
			// same-account retry usually succeeds and keeps the turn on the
			// account the session is anchored to.
			dispatch := loop.dispatchRequest(item, req)
			var result ChatResult
			var classified Classified
			var err error
			for {
				result, classified, err = e.chatInProcessNonStreamAttempt(ctx, item, dispatch, i)
				if err == nil || !loop.canSameAccountRetry(ctx, classified) {
					break
				}
				loop.sameAccountRetries++
				log.Printf("chat same-account retry request_id=%q account=%q code=%q kind=%q retry=%d",
					loop.requestID, poolID, classified.Code, classified.Kind, loop.sameAccountRetries)
			}
			if err == nil {
				result.AttemptCount = i + 1
				e.observeRouting(&loop.routing, poolID)
				// Bind the pool account, not the upstream-reported label. A
				// worker response may rewrite X-Qoder-Account, but session
				// affinity and future Pool.ByID lookups are keyed by the pool ID.
				e.bindSession(loop.routing, poolID)
				release()
				return result, nil
			}
			loop.lastErr = err
			if requestContextDone(ctx, err) {
				release()
				return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			if loop.canFailover(classified) {
				release()
				loop.failover(e, item, classified)
				continue
			}
			release()
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
		}
		payload, err := json.Marshal(buildWorkerChatPayload(sanitizeForItem(item, loop.dispatchRequest(item, req)), false))
		if err != nil {
			release()
			return ChatResult{}, err
		}
		headerAccount := loop.headerAccount(item, i)
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
			release()
			return ChatResult{}, err
		}
		started := time.Now()
		resp, err := e.HTTPClient.Do(httpReq)
		if err != nil {
			if requestContextDone(ctx, err) {
				latency := int(time.Since(started).Milliseconds())
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
					Status: accounts.AttemptStatusError, ErrorKind: accounts.KindUnavailable, ErrorMessage: err.Error(), LatencyMs: &latency,
				})
				release()
				return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			classified := Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			loop.lastErr = NewExecutionError(classified, fmt.Errorf("worker %s request failed: %w", item.ID, err))
			e.markClassified(item.ID, classified, loop.attemptModel(item, req))
			latency := int(time.Since(started).Milliseconds())
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
				Status: accounts.AttemptStatusFailover, ErrorKind: classified.Kind, ErrorMessage: loop.lastErr.Error(), LatencyMs: &latency,
			})
			release()
			loop.failover(e, item, classified)
			continue
		}
		// Non-stream worker responses are buffered whole; cap them so a
		// pathological upstream cannot pin unbounded memory per request.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxWorkerResponseBytes))
		resp.Body.Close()
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
		if account := resp.Header.Get("X-Qoder-Account"); account != "" {
			item.ID = account
		}
		poolItem := item
		poolItem.ID = poolID
		if resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(body))
			classified := classifyWorkerErr(resp, msg)
			loop.lastErr = NewExecutionError(classified, nil)
			if classified.Kind == accounts.KindModelNotAvailable {
				e.handleModelAvailabilityFailure(RequestIDFromContext(ctx), "worker_non_stream", poolID, loop.attemptModel(item, req), classified)
			}
			e.markClassified(poolID, classified, loop.attemptModel(item, req))
			status := accounts.AttemptStatusError
			if loop.canFailover(classified) {
				status = accounts.AttemptStatusFailover
				loop.failover(e, poolItem, classified)
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
					Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
					ErrorMessage: truncateErr(msg), LatencyMs: &latency,
				})
				release()
				continue
			}
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
				ErrorMessage: truncateErr(msg), LatencyMs: &latency,
			})
			release()
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: loop.resultProvider(item)}, loop.lastErr
		}
		result, err := decodeChatResult(req, body)
		if err != nil {
			// A body this account answered with but this gateway cannot decode is
			// an upstream protocol failure, not a routing miss. Classify it here
			// so the HTTP layer does not fall back to a bare 502 "upstream_error"
			// for a problem that is already understood.
			classified := Classified{
				Kind: accounts.KindUnavailable, Status: http.StatusBadGateway,
				Code: "upstream_decode_error", Type: "api_error", Message: err.Error(),
			}
			loop.lastErr = NewExecutionError(classified, err)
			e.markClassified(poolID, classified, loop.attemptModel(item, req))
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: accounts.AttemptStatusError, HTTPStatus: ptrInt(resp.StatusCode),
				ErrorKind: accounts.KindUnavailable, ErrorMessage: truncateErr(err.Error()), LatencyMs: &latency,
			})
			release()
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: loop.resultProvider(item)}, loop.lastErr
		}
		result.AccountID = item.ID
		result.Provider = loop.resultProvider(item)
		result.AttemptCount = i + 1
		result.ReasoningLevel = RequestedReasoningLevel(req)
		if result.ReasoningLevel != "" {
			logResolvedReasoning(ctx, "chat_non_stream", item, req.Model, result.ReasoningLevel)
		}
		e.observeRouting(&loop.routing, poolID)
		e.bindSession(loop.routing, poolID)
		e.markOK(poolID, loop.attemptModel(item, req))
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(resp.StatusCode), LatencyMs: &latency,
			PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
			UsageSource: result.UsageSource,
		})
		release()
		return result, nil
	}
	if loop.lastErr == nil {
		// Exhausting the loop without a selected account is a routing outcome,
		// not an origin outage. Report it with the routing taxonomy (503 +
		// Retry-After) instead of handing the HTTP layer a bare error it would
		// render as 502 upstream_error.
		loop.lastErr = noAccountsError("no worker accounts available")
	}
	return ChatResult{AttemptCount: loop.attempts}, loop.lastErr
}

// sanitizeForItem applies the account-level system-prompt policy. Provider
// families with upstream content screening (WorkBuddy) strip caller system
// prompts when the account opts in; Qoder workers intentionally preserve them.
// WorkBuddy still needs a leading system slot after the strip (code 11128);
// the adapter inserts an empty placeholder, this helper only drops caller text.
func sanitizeForItem(item Item, req translate.ChatRequest) translate.ChatRequest {
	req.Model = AccountModelID(item, req.Model)
	if native := NativeModelID(item, req.Model); native != "" {
		req.Model = native
	}
	if item.DropSystemPrompt && itemProvider(item) != "qoder" {
		return translate.DropSystemMessages(req)
	}
	return req
}

func (e ChatExecutor) chatInProcessNonStreamAttempt(ctx context.Context, item Item, req translate.ChatRequest, attemptIndex int) (ChatResult, Classified, error) {
	adapter, _ := e.Providers.Get(itemProvider(item))
	if adapter.Chat == nil {
		return ChatResult{}, Classified{}, fmt.Errorf("provider %s does not implement chat", item.Provider)
	}
	started := time.Now()
	outcome, err := adapter.Chat.ChatNonStream(ctx, item.ID, sanitizeForItem(item, req))
	finished := time.Now().UTC()
	latency := int(finished.Sub(started).Milliseconds())
	if err == nil {
		logResolvedReasoning(ctx, "chat_non_stream", item, req.Model, outcome.ReasoningLevel)
	}
	if err != nil {
		if requestContextDone(ctx, err) {
			return ChatResult{AccountID: item.ID, Provider: item.Provider}, Classified{Kind: accounts.KindUnavailable, Message: err.Error()}, err
		}
		classified := e.classifyInProcessError(err)
		if classified.Kind == accounts.KindModelNotAvailable {
			e.handleModelAvailabilityFailure(RequestIDFromContext(ctx), "provider_non_stream", item.ID, req.Model, classified)
		}
		e.markClassified(item.ID, classified, req.Model)
		status := accounts.AttemptStatusError
		if classified.Failover {
			status = accounts.AttemptStatusFailover
		}
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: status, ErrorKind: classified.Kind, ErrorMessage: truncateErr(err.Error()), LatencyMs: &latency,
		})
		return ChatResult{AccountID: item.ID, Provider: item.Provider}, classified, NewExecutionError(classified, err)
	}
	e.markOK(item.ID, req.Model)
	e.recordAttempt(ctx, accounts.RequestAttempt{
		AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
		Status: accounts.AttemptStatusOK, LatencyMs: &latency,
		PromptTokens: ptrInt(outcome.PromptTokens), CompletionTokens: ptrInt(outcome.CompletionTokens),
		UsageSource: outcome.UsageSource,
	})
	return ChatResult{
		Model:            outcome.Model,
		Content:          outcome.Content,
		Reasoning:        outcome.Reasoning,
		ToolCalls:        outcome.ToolCalls,
		FinishReason:     outcome.FinishReason,
		PromptTokens:     outcome.PromptTokens,
		CompletionTokens: outcome.CompletionTokens,
		CacheReadTokens:  outcome.CacheReadTokens,
		CacheWriteTokens: outcome.CacheWriteTokens,
		CachedTokens:     outcome.CacheReadTokens,
		UsageSource:      outcome.UsageSource,
		ConsumedCredits:  outcome.Credits,
		AccountID:        item.ID,
		Provider:         item.Provider,
		ReasoningLevel:   outcome.ReasoningLevel,
	}, Classified{}, nil
}

func (e ChatExecutor) chatInProcessStreamAttempt(ctx context.Context, item Item, req translate.ChatRequest, native *translate.NativeResponsesRequest, attemptIndex int, preferNativeResponses bool) (StreamResult, Classified, error) {
	adapter, ok := e.Providers.Get(itemProvider(item))
	if !ok {
		return StreamResult{}, Classified{}, fmt.Errorf("provider %s is not registered", item.Provider)
	}
	if !preferNativeResponses && adapter.Chat == nil {
		return StreamResult{}, Classified{}, fmt.Errorf("provider %s does not implement chat", item.Provider)
	}
	if preferNativeResponses && native != nil && adapter.NativeResponses == nil {
		return StreamResult{}, Classified{}, providers.ErrUnsupported
	}
	started := time.Now()
	var resp *http.Response
	var resolved providers.ResolvedChat
	var err error
	nativeResponses := false
	if preferNativeResponses && native != nil && adapter.NativeResponses != nil {
		options := providers.RequestOptions{
			Model:            NativeModelID(item, AccountModelID(item, req.Model)),
			DropSystemPrompt: item.DropSystemPrompt && itemProvider(item) != "qoder",
		}
		resp, resolved, err = adapter.NativeResponses.ResponsesStream(ctx, item.ID, native, options)
		nativeResponses = err == nil
	} else {
		resp, resolved, err = adapter.Chat.ChatStream(ctx, item.ID, sanitizeForItem(item, req))
	}
	if err == nil {
		logResolvedReasoning(ctx, "chat_stream", item, req.Model, resolved.ReasoningLevel)
	}
	if err != nil {
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
		if errors.Is(err, providers.ErrUnsupported) {
			return StreamResult{AccountID: item.ID, Provider: item.Provider}, Classified{Kind: accounts.KindInvalidRequest, Message: err.Error()}, err
		}
		if requestContextDone(ctx, err) {

			return StreamResult{AccountID: item.ID, Provider: item.Provider}, Classified{Kind: accounts.KindUnavailable, Message: err.Error()}, err
		}
		classified := e.classifyInProcessError(err)
		if classified.Kind == accounts.KindModelNotAvailable {
			e.handleModelAvailabilityFailure(RequestIDFromContext(ctx), "provider_stream", item.ID, req.Model, classified)
		}
		e.markClassified(item.ID, classified, req.Model)
		status := accounts.AttemptStatusError
		if classified.Failover {
			status = accounts.AttemptStatusFailover
		}
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: status, ErrorKind: classified.Kind, ErrorMessage: truncateErr(err.Error()), LatencyMs: &latency,
		})
		return StreamResult{AccountID: item.ID, Provider: item.Provider}, classified, NewExecutionError(classified, err)
	}
	probed, probeClassified, probeErr := probeStreamBody(ctx, resp.Body)
	if probeErr != nil {
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
		if requestContextDone(ctx, probeErr) {
			// The client went away before the first event. Report the
			// cancellation and leave the account's cooldown untouched.
			classified := ClassifyError(probeErr)
			return StreamResult{AccountID: item.ID, Provider: item.Provider}, classified, NewExecutionError(classified, probeErr)
		}
		classified := probeClassified
		if classified.Kind == "" {
			classified = Classify(0, probeErr.Error(), "", accounts.KindUnavailable, "")
		}
		e.markClassified(item.ID, classified, req.Model)
		status := accounts.AttemptStatusError
		if classified.Failover {
			status = accounts.AttemptStatusFailover
		}
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: status, ErrorKind: classified.Kind, ErrorMessage: truncateErr(probeErr.Error()), LatencyMs: &latency,
		})
		return StreamResult{AccountID: item.ID, Provider: item.Provider}, classified, NewExecutionError(classified, probeErr)
	}
	resp.Body = probed
	e.markOK(item.ID, req.Model)
	ttfb := int(time.Since(started).Milliseconds())
	headerAt := time.Now().UTC()
	e.recordAttempt(ctx, accounts.RequestAttempt{
		AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &headerAt,
		Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(http.StatusOK), LatencyMs: &ttfb,
	})
	return StreamResult{Response: resp, AccountID: item.ID, PoolAccountID: item.ID, Provider: item.Provider, TTFBMs: ttfb, ReasoningLevel: resolved.ReasoningLevel, NativeResponses: nativeResponses}, Classified{}, nil
}

func (e ChatExecutor) classifyInProcessError(err error) Classified { return ClassifyError(err) }

func lastAccountID(excluded map[string]struct{}) string {
	for id := range excluded {
		return id
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func firstIntPtr(vals ...*int) *int {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

func decodeChatResult(req translate.ChatRequest, body []byte) (ChatResult, error) {
	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens             int      `json:"prompt_tokens"`
			CompletionTokens         int      `json:"completion_tokens"`
			CacheReadTokens          *int     `json:"cache_read_tokens"`
			CacheReadInputTokens     *int     `json:"cache_read_input_tokens"`
			CachedTokens             *int     `json:"cached_tokens"`
			CachedContentTokenCount  *int     `json:"cached_content_token_count"`
			CacheWriteTokens         *int     `json:"cache_write_tokens"`
			CacheCreationInputTokens *int     `json:"cache_creation_input_tokens"`
			PromptCacheHitTokens     *int     `json:"prompt_cache_hit_tokens"`
			Source                   string   `json:"source"`
			Credits                  *float64 `json:"credits"`
			PromptDetails            struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			InputDetails struct {
				CachedTokens     *int `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          string          `json:"content"`
				ReasoningContent string          `json:"reasoning_content"`
				ToolCalls        json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ChatResult{}, fmt.Errorf("decode worker response: %w; body=%s", err, truncateErr(string(body)))
	}
	content := ""
	reasoning := ""
	var toolCalls json.RawMessage
	finishReason := "stop"
	if len(parsed.Choices) > 0 {
		content = parsed.Choices[0].Message.Content
		reasoning = parsed.Choices[0].Message.ReasoningContent
		toolCalls = parsed.Choices[0].Message.ToolCalls
		if parsed.Choices[0].FinishReason != "" {
			finishReason = parsed.Choices[0].FinishReason
		} else if len(toolCalls) > 0 && string(toolCalls) != "null" {
			finishReason = "tool_calls"
		}
	}
	model := parsed.Model
	if model == "" {
		model = req.Model
	}
	source := parsed.Usage.Source
	if source == "" {
		source = "estimate"
	}
	cacheRead := firstIntPtr(
		parsed.Usage.CacheReadTokens,
		parsed.Usage.CacheReadInputTokens,
		parsed.Usage.PromptDetails.CachedTokens,
		parsed.Usage.InputDetails.CachedTokens,
		parsed.Usage.PromptCacheHitTokens,
		parsed.Usage.CachedTokens,
		parsed.Usage.CachedContentTokenCount,
	)
	cacheWrite := firstIntPtr(
		parsed.Usage.CacheWriteTokens,
		parsed.Usage.CacheCreationInputTokens,
		parsed.Usage.InputDetails.CacheWriteTokens,
	)
	cachedTokens := firstIntPtr(parsed.Usage.PromptDetails.CachedTokens, parsed.Usage.InputDetails.CachedTokens, cacheRead)
	return ChatResult{
		Model:            model,
		Content:          content,
		Reasoning:        reasoning,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CacheReadTokens:  cacheRead,
		CacheWriteTokens: cacheWrite,
		CachedTokens:     cachedTokens,
		UsageSource:      source,
		Credits:          parsed.Usage.Credits,
	}, nil
}

func (e ChatExecutor) streamHTTPClient() *http.Client {
	client := e.HTTPClient
	if client == nil {
		return http.DefaultClient
	}
	if client.Timeout > 0 {
		streamClient := *client
		streamClient.Timeout = 0
		return &streamClient
	}
	return client
}

func (e ChatExecutor) ChatStreamProxy(ctx context.Context, req translate.ChatRequest, prefer, providerFilter string) (result StreamResult, returnErr error) {
	return e.chatStreamProxy(ctx, req, nil, prefer, providerFilter, false)
}

// ChatStreamProxyNativeResponses sends the original Responses request to a
// native-capable adapter. Once this path starts, retries remain native-only.
func (e ChatExecutor) ChatStreamProxyNativeResponses(ctx context.Context, req translate.ChatRequest, native *translate.NativeResponsesRequest, prefer, providerFilter string) (result StreamResult, returnErr error) {
	return e.chatStreamProxy(ctx, req, native, prefer, providerFilter, true)
}

func (e ChatExecutor) NativeResponsesNonStream(ctx context.Context, req translate.ChatRequest, native *translate.NativeResponsesRequest, prefer, providerFilter string) (result NativeResponseResult, returnErr error) {
	upstream, err := e.ChatStreamProxyNativeResponses(ctx, req, native, prefer, providerFilter)
	if err != nil {
		return NativeResponseResult{AccountID: upstream.AccountID, Provider: upstream.Provider, AttemptCount: upstream.AttemptCount, Routing: upstream.Routing, ReasoningLevel: upstream.ReasoningLevel}, err
	}
	if upstream.Response == nil || upstream.Response.Body == nil {
		return NativeResponseResult{AccountID: upstream.AccountID, Provider: upstream.Provider, AttemptCount: upstream.AttemptCount, Routing: upstream.Routing, ReasoningLevel: upstream.ReasoningLevel}, fmt.Errorf("native responses upstream returned no body")
	}
	defer upstream.Response.Body.Close()
	collected, err := translate.CollectResponses(upstream.Response.Body)
	result = NativeResponseResult{
		Response: collected.Response, AccountID: upstream.AccountID, Provider: upstream.Provider,
		AttemptCount: upstream.AttemptCount, Routing: upstream.Routing, ReasoningLevel: upstream.ReasoningLevel,
		FinishReason: collected.FinishReason, PromptTokens: collected.InputTokens,
		OutputTokens: collected.OutputTokens, CachedTokens: collected.CachedTokens,
	}
	if err != nil {
		e.ObserveStreamFailure(poolAccountID(upstream), err, req.Model)
		return result, err
	}
	e.CommitSession(ctx, req, upstream.Routing, poolAccountID(upstream))
	return result, nil
}

func poolAccountID(result StreamResult) string {
	if strings.TrimSpace(result.PoolAccountID) != "" {
		return result.PoolAccountID
	}
	return result.AccountID
}

func (e ChatExecutor) HasNativeResponsesRoute(ctx context.Context, req translate.ChatRequest, prefer, providerFilter string) bool {
	if e.Providers == nil {
		return false
	}
	loop := e.newRouteLoop(ctx, prefer, providerFilter, req)
	loop.eligible = func(item Item) bool {
		adapter, ok := e.Providers.Get(itemProvider(item))
		return ok && adapter.NativeResponses != nil
	}
	if e.Pool == nil {
		return false
	}
	query := e.routeQuery(loop.prefer, loop.providerFilter, loop.regionFilter, req.Model, loop.allowed, nil, loop.eligible, loop.plan)
	return e.Pool.LenRoute(query) > 0
}

func (e ChatExecutor) chatStreamProxy(ctx context.Context, req translate.ChatRequest, native *translate.NativeResponsesRequest, prefer, providerFilter string, preferNativeResponses bool) (result StreamResult, returnErr error) {
	loop := e.newRouteLoop(ctx, prefer, providerFilter, req)
	if preferNativeResponses && native != nil && e.Providers == nil {
		return StreamResult{}, providers.ErrUnsupported
	}
	if preferNativeResponses && native != nil {
		loop.eligible = func(item Item) bool {
			adapter, ok := e.Providers.Get(itemProvider(item))
			return ok && adapter.NativeResponses != nil
		}
		loop.attempts = e.attemptsFor(loop.providerFilter, loop.regionFilter, req.Model, loop.allowed, loop.eligible, loop.resolver)
	}
	defer func() { result.Routing = loop.routing.Source }()
	startedAll := time.Now()
	for loop.index < loop.attempts {
		item, i, err := loop.pickNext(e, req.Model)
		if err != nil {
			attempts, accountID, provider, pickErr := loop.pickFailure(err)
			if loop.lastErr != nil {
				return StreamResult{AttemptCount: attempts, AccountID: accountID, Provider: provider}, pickErr
			}
			return StreamResult{}, pickErr
		}
		noteStreamRoute(ctx, item.ID, loop.resultProvider(item))
		poolID := item.ID
		if !e.acquireInFlight(loop.requestID, poolID) {
			loop.exclude(item)
			continue
		}
		release := func() { e.releaseInFlight(loop.requestID, poolID) }
		if isInProcessItem(item) || (e.qoderDirect(item) && !(preferNativeResponses && native != nil)) {
			dispatch := loop.dispatchRequest(item, req)
			var result StreamResult
			var classified Classified
			var err error
			for {
				result, classified, err = e.chatInProcessStreamAttempt(ctx, item, dispatch, native, i, preferNativeResponses)
				if err == nil || !loop.canSameAccountRetry(ctx, classified) {
					break
				}
				// Nothing has been relayed to the client: the attempt failed
				// before the first meaningful event, so a same-account retry is
				// both safe and cache-warm.
				loop.sameAccountRetries++
				log.Printf("chat stream same-account retry request_id=%q account=%q code=%q kind=%q retry=%d",
					loop.requestID, poolID, classified.Code, classified.Kind, loop.sameAccountRetries)
			}

			if err == nil {
				result.AttemptCount = i + 1
				e.observeRouting(&loop.routing, poolID)
				// Bind at response headers, not after the stream body ends. A
				// follow-up turn can start while the first stream is still
				// running; waiting for [DONE] would let it select another
				// account and lose the prompt cache. Bind the pool ID because
				// the upstream-reported label is not a Pool routing key.
				e.bindSession(loop.routing, poolID)
				result.Response = e.attachInFlightRelease(result.Response, loop.requestID, poolID)
				return result, nil
			}
			loop.lastErr = err
			if requestContextDone(ctx, err) {
				release()
				return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			if loop.canFailover(classified) {
				release()
				loop.failover(e, item, classified)
				continue
			}
			release()
			return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
		}
		payload, err := json.Marshal(buildWorkerChatPayload(sanitizeForItem(item, loop.dispatchRequest(item, req)), true))
		if err != nil {
			release()
			return StreamResult{}, err
		}
		headerAccount := loop.headerAccount(item, i)
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
			release()
			return StreamResult{}, err
		}
		started := time.Now()
		resp, err := e.streamHTTPClient().Do(httpReq)
		if err != nil {
			if requestContextDone(ctx, err) {
				latency := int(time.Since(started).Milliseconds())
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
					Status: accounts.AttemptStatusError, ErrorKind: accounts.KindUnavailable, ErrorMessage: err.Error(), LatencyMs: &latency,
				})
				release()
				return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			classified := Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			loop.lastErr = NewExecutionError(classified, fmt.Errorf("worker %s stream request failed: %w", item.ID, err))
			e.markClassified(item.ID, classified, loop.attemptModel(item, req))
			latency := int(time.Since(started).Milliseconds())
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
				Status: accounts.AttemptStatusFailover, ErrorKind: classified.Kind, ErrorMessage: loop.lastErr.Error(), LatencyMs: &latency,
			})
			release()
			loop.failover(e, item, classified)
			continue
		}
		if account := resp.Header.Get("X-Qoder-Account"); account != "" {
			item.ID = account
			noteStreamRoute(ctx, item.ID, loop.resultProvider(item))
		}
		poolItem := item
		poolItem.ID = poolID
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxWorkerResponseBytes))
			resp.Body.Close()
			msg := strings.TrimSpace(string(body))
			classified := classifyWorkerErr(resp, msg)
			loop.lastErr = NewExecutionError(classified, nil)
			if classified.Kind == accounts.KindModelNotAvailable {
				e.handleModelAvailabilityFailure(RequestIDFromContext(ctx), "worker_stream", poolID, loop.attemptModel(item, req), classified)
			}
			e.markClassified(poolID, classified, loop.attemptModel(item, req))
			finished := time.Now().UTC()
			latency := int(finished.Sub(started).Milliseconds())
			status := accounts.AttemptStatusError
			if loop.canFailover(classified) {
				status = accounts.AttemptStatusFailover
				loop.failover(e, poolItem, classified)
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
					Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
					ErrorMessage: truncateErr(msg), LatencyMs: &latency,
				})
				release()
				continue
			}
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
				ErrorMessage: truncateErr(msg), LatencyMs: &latency,
			})
			release()
			return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: loop.resultProvider(item)}, loop.lastErr
		}
		probed, probeClassified, probeErr := probeStreamBody(ctx, resp.Body)
		if probeErr != nil {
			if requestContextDone(ctx, probeErr) {
				// The client went away before the first event. Record the
				// cancellation without cooling the account or failing over.
				classified := ClassifyError(probeErr)
				latency := int(time.Since(started).Milliseconds())
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
					Status: accounts.AttemptStatusError, ErrorKind: classified.Kind, ErrorMessage: probeErr.Error(), LatencyMs: &latency,
				})
				release()
				return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, NewExecutionError(classified, probeErr)
			}
			classified := probeClassified
			if classified.Kind == "" {
				classified = Classify(0, probeErr.Error(), "", accounts.KindUnavailable, "")
			}
			loop.lastErr = NewExecutionError(classified, fmt.Errorf("worker %s stream failed before the first event: %w", item.ID, probeErr))
			e.markClassified(poolID, classified, loop.attemptModel(item, req))
			finished := time.Now().UTC()
			latency := int(finished.Sub(started).Milliseconds())
			status := accounts.AttemptStatusError
			if loop.canFailover(classified) {
				status = accounts.AttemptStatusFailover
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
					Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
					ErrorMessage: truncateErr(probeErr.Error()), LatencyMs: &latency,
				})
				loop.failover(e, poolItem, classified)
				release()
				continue
			}
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
				ErrorMessage: truncateErr(probeErr.Error()), LatencyMs: &latency,
			})
			release()
			return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: loop.resultProvider(item)}, loop.lastErr
		}
		resp.Body = probed
		e.markOK(poolID, loop.attemptModel(item, req))
		ttfb := int(time.Since(startedAll).Milliseconds())
		headerAt := time.Now().UTC()
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &headerAt,
			Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(resp.StatusCode), LatencyMs: &ttfb,
		})
		e.observeRouting(&loop.routing, poolID)
		e.bindSession(loop.routing, poolID)
		resolved := RequestedReasoningLevel(req)
		if resolved != "" {
			logResolvedReasoning(ctx, "chat_stream", item, req.Model, resolved)
		}
		resp = e.attachInFlightRelease(resp, loop.requestID, poolID)
		return StreamResult{
			Response:       resp,
			AccountID:      item.ID,
			PoolAccountID:  poolID,
			Provider:       loop.resultProvider(item),
			AttemptCount:   i + 1,
			TTFBMs:         ttfb,
			ReasoningLevel: resolved,
		}, nil
	}
	if loop.lastErr == nil {
		// Exhausting the loop without a selected account is a routing outcome,
		// not an origin outage. Report it with the routing taxonomy (503 +
		// Retry-After) instead of handing the HTTP layer a bare error it would
		// render as 502 upstream_error.
		loop.lastErr = noAccountsError("no worker accounts available")
	}
	return StreamResult{AttemptCount: loop.attempts}, loop.lastErr
}

func ptrInt(value int) *int { return &value }

func ptrTime(value time.Time) *time.Time { return &value }

func truncateErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) <= 500 {
		return msg
	}
	return msg[:500]
}

// logResolvedReasoning emits one line per successful chat attempt showing the
// reasoning level actually sent upstream. Empty levels are omitted to keep
// noise down for providers that do not accept a reasoning knob.
func logResolvedReasoning(ctx context.Context, source string, item Item, model, level string) {
	if level == "" {
		return
	}
	log.Printf("chat reasoning resolved request_id=%q source=%q account=%q provider=%q model=%q level=%q",
		RequestIDFromContext(ctx), source, item.ID, item.Provider, model, level)
}
