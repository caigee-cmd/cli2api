package executor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder"
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
}

type StreamResult struct {
	Response     *http.Response
	AccountID    string
	Provider     string
	AttemptCount int
	TTFBMs       int
	Routing      string
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

func requestContextDone(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (ctx != nil && ctx.Err() != nil)
}

const (
	routingPool         = "pool"
	routingPin          = "pin"
	routingSticky       = "sticky"
	routingStickyEscape = "sticky_escape"
)

type routingPlan struct {
	Source       string
	SessionKey   string
	BoundAccount string
	PublicModel  string
}

func (e ChatExecutor) bindSession(plan routingPlan, accountID string) {
	if plan.Source == routingPin || plan.SessionKey == "" || e.SessionAffinity == nil {
		return
	}
	e.SessionAffinity.Bind(plan.SessionKey, accountID)
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
	e.SessionAffinity.Bind(resolveSessionKey(ctx, req), accountID)
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

func (e ChatExecutor) prepareRouting(ctx context.Context, prefer, providerFilter string, req translate.ChatRequest) (string, string, string, routingPlan) {
	prefer = strings.TrimSpace(prefer)
	providerFilter = strings.ToLower(strings.TrimSpace(providerFilter))
	publicModel := req.Model
	if prefer != "" {
		return prefer, providerFilter, "", routingPlan{Source: routingPin, PublicModel: publicModel}
	}

	plan := routingPlan{Source: routingPool, SessionKey: resolveSessionKey(ctx, req), PublicModel: publicModel}
	if plan.SessionKey == "" || e.SessionAffinity == nil || e.Pool == nil {
		return "", providerFilter, "", plan
	}
	accountID, ok := e.SessionAffinity.Get(plan.SessionKey)
	if !ok {
		return "", providerFilter, "", plan
	}
	item, ok := e.Pool.ByID(accountID)
	if !ok {
		e.SessionAffinity.RecordEscape("account_missing")
		e.SessionAffinity.Forget(plan.SessionKey)
		return "", providerFilter, "", plan
	}
	if providerFilter != "" && providerFilter != itemProvider(item) {
		e.SessionAffinity.RecordEscape("provider_mismatch")
		return "", providerFilter, "", plan
	}
	if !accounts.ProviderRegionAllowed(itemProvider(item), accounts.NormalizeRegion(item.Region), allowedProvidersFrom(ctx)) {
		e.SessionAffinity.RecordEscape("provider_not_allowed")
		return "", providerFilter, "", plan
	}
	if !stickyAccountCanServeModel(item, publicModel) {
		e.SessionAffinity.RecordEscape("model_unavailable")
		return "", providerFilter, "", plan
	}
	return item.ID, itemProvider(item), accounts.NormalizeRegion(item.Region), routingPlan{
		Source: routingSticky, SessionKey: plan.SessionKey, BoundAccount: item.ID, PublicModel: publicModel,
	}
}

func NewChatExecutor(pool *Pool, workerKey string) ChatExecutor {
	if pool == nil {
		pool = NewPool(nil, nil)
	}
	return ChatExecutor{
		Pool:            pool,
		WorkerKey:       strings.TrimSpace(workerKey),
		MaxAttempts:     4,
		SessionAffinity: NewSessionAffinity(defaultSessionAffinityTTL, defaultSessionAffinityCapacity),
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (e ChatExecutor) routeQuery(prefer, providerFilter, regionFilter, publicModel string, allowed []string, excluded map[string]struct{}) RouteQuery {
	return RouteQuery{
		PublicModel:      publicModel,
		PreferAccount:    prefer,
		ProviderFilter:   providerFilter,
		RegionFilter:     regionFilter,
		AllowedProviders: allowed,
		Excluded:         excluded,
	}
}

func (e ChatExecutor) pick(requestID, prefer, providerFilter, regionFilter, publicModel string, allowed []string, excluded map[string]struct{}) (Item, error) {
	query := e.routeQuery(prefer, providerFilter, regionFilter, publicModel, allowed, excluded)
	if e.Pool != nil {
		if item, ok := e.Pool.PickRoute(query); ok {
			if retryAfter := e.Pool.RetryAfter(item, publicModel); retryAfter > 0 {
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
			failover := true
			return Item{}, &providers.Error{
				Kind: accounts.KindRateLimit, Status: 429, Code: "rate_limit",
				Type: "api_error", Message: "all accounts at capacity",
				Cooldown: 5 * time.Second, RetryAfter: 5 * time.Second, Failover: &failover,
			}
		}
		if publicModel != "" && publicModel != "auto" {
			unfiltered := query
			unfiltered.PublicModel = ""
			if e.Pool.LenRoute(unfiltered) > 0 {
				e.logModelRouteMiss(requestID, query)
				return Item{}, fmt.Errorf("model_not_available: %s is not available for the selected accounts", publicModel)
			}
		}
	}
	if len(allowed) > 0 && providerFilter != "" && !accounts.ProviderAllowed(providerFilter, allowed) {
		return Item{}, fmt.Errorf("api key cannot use provider %s", providerFilter)
	}
	if providerFilter != "" && regionFilter != "" {
		return Item{}, fmt.Errorf("no %s/%s accounts available", providerFilter, regionFilter)
	}
	if providerFilter != "" {
		// A key whose grant list narrows this family to a single region
		// should fail with that region in the message, even when the
		// request itself did not pin one.
		if regionFilter, narrowed := keyGrantedSingleRegion(providerFilter, allowed); narrowed {
			return Item{}, fmt.Errorf("no %s/%s accounts available", providerFilter, regionFilter)
		}
		return Item{}, fmt.Errorf("no %s accounts available", providerFilter)
	}
	if len(allowed) > 0 {
		return Item{}, fmt.Errorf("no accounts available for this api key")
	}
	return Item{}, fmt.Errorf("no worker accounts configured")
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
		failover = false
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
	return &providers.Error{
		Kind: kind, Status: 429, Code: code, Type: typ, Message: message,
		Cooldown: retryAfter, RetryAfter: retryAfter, Failover: &failover,
	}
}

func (e ChatExecutor) attemptsFor(providerFilter, regionFilter, publicModel string, allowed []string) int {
	maxAttempts := e.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 4
	}
	if maxAttempts > 64 {
		maxAttempts = 64
	}
	if e.Pool != nil {
		if n := e.Pool.LenRoute(e.routeQuery("", providerFilter, regionFilter, publicModel, allowed, nil)); n > 0 {
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
	if e.Pool == nil || accountID == "" || err == nil {
		return
	}
	classified := e.classifyInProcessError(err)
	if classified.Kind == "" {
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
	if classified.Kind == accounts.KindQuota {
		// Quota is classified with Failover=false because the account is not
		// at fault on a normal request path. Here the response already went
		// out with 200, so there is no other account to fail over to; without
		// an explicit cooldown the next request would pick this account again
		// and fail the same way. Force the cooldown.
		classified.Failover = true
		if classified.Cooldown <= 0 {
			classified.Cooldown = NextLocalMidnightCooldown()
		}
	}
	e.markClassified(accountID, classified, model)
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
		c.Model = model
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
	e.Pool.MarkOK(id, model)
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

func (e ChatExecutor) newWorkerRequest(ctx context.Context, item Item, payload []byte, prefer string) (*http.Request, error) {
	account := prefer
	if account == "" {
		account = item.ID
	}
	key := e.WorkerKey
	if e.WorkerKeySource != nil {
		key = e.WorkerKeySource()
	}
	return qoder.NewChatRequest(ctx, item.URL, account, RequestIDFromContext(ctx), key, payload)
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
}

func (e ChatExecutor) newRouteLoop(ctx context.Context, prefer, providerFilter string, req translate.ChatRequest) routeLoop {
	prefer, providerFilter, regionFilter, routing := e.prepareRouting(ctx, prefer, providerFilter, req)
	allowed := allowedProvidersFrom(ctx)
	// A key whose grants narrow the selected family to exactly one region
	// pre-seeds the region filter: scheduling never even considers accounts
	// of other regions — including a pin to an out-of-grant account (the
	// grant overrides the pinned account's region, so the pin falls back
	// into the granted region) and every failover hop.
	if providerFilter != "" {
		if granted, narrowed := keyGrantedSingleRegion(providerFilter, allowed); narrowed {
			regionFilter = granted
		}
	}
	if regionFilter == "" && prefer != "" && e.Pool != nil {
		if pinnedItem, ok := e.Pool.ByID(prefer); ok {
			regionFilter = pinRegion("", pinnedItem.Region)
		}
	}
	loop := routeLoop{
		requestID:      RequestIDFromContext(ctx),
		prefer:         prefer,
		providerFilter: providerFilter,
		regionFilter:   regionFilter,
		routing:        routing,
		excluded:       map[string]struct{}{},
		allowed:        allowed,
		attempts:       e.attemptsFor(providerFilter, regionFilter, req.Model, allowed),
	}
	if routing.Source == routingPin {
		loop.pinned = prefer
	}
	return loop
}

func (l *routeLoop) pickNext(e ChatExecutor, publicModel string) (Item, int, error) {
	item, err := e.pick(l.requestID, l.prefer, l.providerFilter, l.regionFilter, publicModel, l.allowed, l.excluded)
	if err != nil {
		return Item{}, l.index, err
	}
	attemptIndex := l.index
	e.observeRouting(&l.routing, item.ID)
	l.prefer = ""
	if l.regionFilter == "" {
		l.regionFilter = pinRegion(l.regionFilter, item.Region)
		l.attempts = e.attemptsFor(l.providerFilter, l.regionFilter, publicModel, l.allowed)
	}
	l.index++
	return item, attemptIndex, nil
}

func (l routeLoop) canFailover(classified Classified) bool {
	return classified.Failover && l.index < l.attempts
}

func (l *routeLoop) exclude(item Item) {
	l.excluded[item.ID] = struct{}{}
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

func (e ChatExecutor) ChatNonStream(ctx context.Context, req translate.ChatRequest, prefer, providerFilter string) (result ChatResult, returnErr error) {
	loop := e.newRouteLoop(ctx, prefer, providerFilter, req)
	defer func() { result.Routing = loop.routing.Source }()
	payload, err := json.Marshal(qoder.BuildChatPayload(req, false))
	if err != nil {
		return ChatResult{}, err
	}
	for loop.index < loop.attempts {
		item, i, err := loop.pickNext(e, req.Model)
		if err != nil {
			attempts, accountID, provider, pickErr := loop.pickFailure(err)
			if loop.lastErr != nil {
				return ChatResult{AttemptCount: attempts, AccountID: accountID, Provider: provider}, pickErr
			}
			return ChatResult{}, pickErr
		}
		if isInProcessItem(item) {
			result, classified, err := e.chatInProcessNonStreamAttempt(ctx, item, req, i)
			if err == nil {
				result.AttemptCount = i + 1
				e.observeRouting(&loop.routing, result.AccountID)
				e.bindSession(loop.routing, result.AccountID)
				return result, nil
			}
			loop.lastErr = err
			if requestContextDone(ctx, err) {
				return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			if loop.canFailover(classified) {
				loop.exclude(item)
				continue
			}
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
		}
		headerAccount := loop.headerAccount(item, i)
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
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
				return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			classified := Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			loop.lastErr = fmt.Errorf("worker %s request failed: %w", item.ID, err)
			e.markClassified(item.ID, classified, req.Model)
			latency := int(time.Since(started).Milliseconds())
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
				Status: accounts.AttemptStatusFailover, ErrorKind: classified.Kind, ErrorMessage: loop.lastErr.Error(), LatencyMs: &latency,
			})
			loop.exclude(item)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
		if account := resp.Header.Get("X-Qoder-Account"); account != "" {
			item.ID = account
		}
		if resp.StatusCode >= 300 {
			msg := strings.TrimSpace(string(body))
			classified := classifyWorkerErr(resp, msg)
			loop.lastErr = ProviderErrorFromClassified(classified)
			if classified.Kind == accounts.KindModelNotAvailable {
				e.handleModelAvailabilityFailure(RequestIDFromContext(ctx), "worker_non_stream", item.ID, req.Model, classified)
			}
			e.markClassified(item.ID, classified, req.Model)
			status := accounts.AttemptStatusError
			if loop.canFailover(classified) {
				status = accounts.AttemptStatusFailover
				loop.exclude(item)
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
					Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
					ErrorMessage: truncateErr(msg), LatencyMs: &latency,
				})
				continue
			}
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
				ErrorMessage: truncateErr(msg), LatencyMs: &latency,
			})
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: loop.resultProvider(item)}, loop.lastErr
		}
		result, err := decodeChatResult(req, body)
		if err != nil {
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: accounts.AttemptStatusError, HTTPStatus: ptrInt(resp.StatusCode),
				ErrorKind: accounts.KindUnavailable, ErrorMessage: truncateErr(err.Error()), LatencyMs: &latency,
			})
			return ChatResult{AttemptCount: i + 1, AccountID: item.ID, Provider: loop.resultProvider(item)}, err
		}
		result.AccountID = item.ID
		result.Provider = loop.resultProvider(item)
		result.AttemptCount = i + 1
		e.observeRouting(&loop.routing, item.ID)
		e.bindSession(loop.routing, item.ID)
		e.markOK(item.ID, req.Model)
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
			Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(resp.StatusCode), LatencyMs: &latency,
			PromptTokens: ptrInt(result.PromptTokens), CompletionTokens: ptrInt(result.CompletionTokens),
			UsageSource: result.UsageSource,
		})
		return result, nil
	}
	if loop.lastErr == nil {
		loop.lastErr = fmt.Errorf("no worker accounts available")
	}
	return ChatResult{AttemptCount: loop.attempts}, loop.lastErr
}

// sanitizeForItem applies the account-level system-prompt policy. Provider
// families with upstream content screening (WorkBuddy) strip caller system
// prompts when the account opts in; Qoder workers intentionally preserve them.
// WorkBuddy still needs a leading system slot after the strip (code 11128);
// the adapter inserts an empty placeholder, this helper only drops caller text.
func sanitizeForItem(item Item, req translate.ChatRequest) translate.ChatRequest {
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
		return ChatResult{AccountID: item.ID, Provider: item.Provider}, classified, providerErrorFor(err, classified)
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
		UsageSource:      outcome.UsageSource,
		ConsumedCredits:  outcome.Credits,
		AccountID:        item.ID,
		Provider:         item.Provider,
	}, Classified{}, nil
}

func (e ChatExecutor) chatInProcessStreamAttempt(ctx context.Context, item Item, req translate.ChatRequest, attemptIndex int) (StreamResult, Classified, error) {
	adapter, _ := e.Providers.Get(itemProvider(item))
	if adapter.Chat == nil {
		return StreamResult{}, Classified{}, fmt.Errorf("provider %s does not implement chat", item.Provider)
	}
	started := time.Now()
	resp, err := adapter.Chat.ChatStream(ctx, item.ID, sanitizeForItem(item, req))
	if err != nil {
		finished := time.Now().UTC()
		latency := int(finished.Sub(started).Milliseconds())
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
		return StreamResult{AccountID: item.ID, Provider: item.Provider}, classified, providerErrorFor(err, classified)
	}
	e.markOK(item.ID, req.Model)
	ttfb := int(time.Since(started).Milliseconds())
	headerAt := time.Now().UTC()
	e.recordAttempt(ctx, accounts.RequestAttempt{
		AttemptIndex: attemptIndex, AccountID: item.ID, StartedAt: started, FinishedAt: &headerAt,
		Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(http.StatusOK), LatencyMs: &ttfb,
	})
	return StreamResult{Response: resp, AccountID: item.ID, Provider: item.Provider, TTFBMs: ttfb}, Classified{}, nil
}

// ProviderErrorFromClassified preserves routing classification at the provider
// error boundary, including the cooldown fallback for missing Retry-After.
func ProviderErrorFromClassified(classified Classified) *providers.Error {
	failover := classified.Failover
	retryAfter := classified.RetryAfter
	if retryAfter <= 0 {
		retryAfter = classified.Cooldown
	}
	return &providers.Error{
		Kind:       classified.Kind,
		Status:     classified.Status,
		Message:    classified.Message,
		Code:       classified.Code,
		Type:       classified.Type,
		Cooldown:   classified.Cooldown,
		RetryAfter: retryAfter,
		Failover:   &failover,
	}
}

func providerErrorFor(err error, classified Classified) error {
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr == nil {
		return err
	}
	return ProviderErrorFromClassified(classified)
}

func (e ChatExecutor) classifyInProcessError(err error) Classified {
	if err == nil {
		return Classify(0, "", "", accounts.KindUnavailable, "")
	}
	var providerErr *providers.Error
	if !errors.As(err, &providerErr) || providerErr == nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Classified{
				Kind: accounts.KindCanceled, Status: 499, Failover: false,
				Code: "request_canceled", Message: err.Error(),
			}
		}
		return Classify(0, err.Error(), "", accounts.KindUnavailable, "")
	}
	message := strings.TrimSpace(providerErr.Message)
	if message == "" {
		message = providerErr.Error()
	}
	raw := strings.TrimSpace(strings.Join([]string{message, providerErr.Code, providerErr.Type}, " "))
	failoverHint := ""
	if providerErr.Failover != nil {
		if *providerErr.Failover {
			failoverHint = "1"
		} else {
			failoverHint = "0"
		}
	}
	classified := Classify(providerErr.Status, raw, "", providerErr.Kind, failoverHint)
	if providerErr.Code != "" {
		classified.Code = providerErr.Code
	}
	if providerErr.Type != "" {
		classified.Type = providerErr.Type
	}
	if providerErr.Message != "" {
		classified.Message = providerErr.Message
	}
	providerRetryAfter := providerErr.RetryAfter
	if providerRetryAfter <= 0 {
		providerRetryAfter = providerErr.Cooldown
	}
	if providerRetryAfter > 0 {
		classified.Cooldown = providerRetryAfter
		if classified.Kind == accounts.KindRateLimit && classified.Cooldown < 30*time.Second {
			classified.Cooldown = 30 * time.Second
		}
	}
	classified.RetryAfter = classified.Cooldown
	return classified
}

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

func decodeChatResult(req translate.ChatRequest, body []byte) (ChatResult, error) {
	var parsed struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int      `json:"prompt_tokens"`
			CompletionTokens int      `json:"completion_tokens"`
			CacheReadTokens  *int     `json:"cache_read_tokens"`
			CacheWriteTokens *int     `json:"cache_write_tokens"`
			Source           string   `json:"source"`
			Credits          *float64 `json:"credits"`
			PromptDetails    struct {
				CachedTokens *int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
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
		return ChatResult{}, fmt.Errorf("decode worker response: %w; body=%s", err, string(body))
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
	return ChatResult{
		Model:            model,
		Content:          content,
		Reasoning:        reasoning,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		CacheReadTokens:  parsed.Usage.CacheReadTokens,
		CacheWriteTokens: parsed.Usage.CacheWriteTokens,
		CachedTokens:     parsed.Usage.PromptDetails.CachedTokens,
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
	loop := e.newRouteLoop(ctx, prefer, providerFilter, req)
	defer func() { result.Routing = loop.routing.Source }()
	payload, err := json.Marshal(qoder.BuildChatPayload(req, true))
	if err != nil {
		return StreamResult{}, err
	}
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
		if isInProcessItem(item) {
			result, classified, err := e.chatInProcessStreamAttempt(ctx, item, req, i)
			if err == nil {
				result.AttemptCount = i + 1
				e.observeRouting(&loop.routing, result.AccountID)
				return result, nil
			}
			loop.lastErr = err
			if requestContextDone(ctx, err) {
				return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			if loop.canFailover(classified) {
				loop.exclude(item)
				continue
			}
			return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
		}
		headerAccount := loop.headerAccount(item, i)
		httpReq, err := e.newWorkerRequest(ctx, item, payload, headerAccount)
		if err != nil {
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
				return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: item.Provider}, err
			}
			classified := Classify(0, err.Error(), "", accounts.KindUnavailable, "")
			loop.lastErr = fmt.Errorf("worker %s stream request failed: %w", item.ID, err)
			e.markClassified(item.ID, classified, req.Model)
			latency := int(time.Since(started).Milliseconds())
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: ptrTime(time.Now().UTC()),
				Status: accounts.AttemptStatusFailover, ErrorKind: classified.Kind, ErrorMessage: loop.lastErr.Error(), LatencyMs: &latency,
			})
			loop.exclude(item)
			continue
		}
		if account := resp.Header.Get("X-Qoder-Account"); account != "" {
			item.ID = account
		}
		if resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			msg := strings.TrimSpace(string(body))
			classified := classifyWorkerErr(resp, msg)
			loop.lastErr = ProviderErrorFromClassified(classified)
			if classified.Kind == accounts.KindModelNotAvailable {
				e.handleModelAvailabilityFailure(RequestIDFromContext(ctx), "worker_stream", item.ID, req.Model, classified)
			}
			e.markClassified(item.ID, classified, req.Model)
			finished := time.Now().UTC()
			latency := int(finished.Sub(started).Milliseconds())
			status := accounts.AttemptStatusError
			if loop.canFailover(classified) {
				status = accounts.AttemptStatusFailover
				loop.exclude(item)
				e.recordAttempt(ctx, accounts.RequestAttempt{
					AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
					Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
					ErrorMessage: truncateErr(msg), LatencyMs: &latency,
				})
				continue
			}
			e.recordAttempt(ctx, accounts.RequestAttempt{
				AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &finished,
				Status: status, HTTPStatus: ptrInt(resp.StatusCode), ErrorKind: classified.Kind,
				ErrorMessage: truncateErr(msg), LatencyMs: &latency,
			})
			return StreamResult{AttemptCount: i + 1, AccountID: item.ID, Provider: loop.resultProvider(item)}, loop.lastErr
		}
		e.markOK(item.ID, req.Model)
		ttfb := int(time.Since(startedAll).Milliseconds())
		headerAt := time.Now().UTC()
		e.recordAttempt(ctx, accounts.RequestAttempt{
			AttemptIndex: i, AccountID: item.ID, StartedAt: started, FinishedAt: &headerAt,
			Status: accounts.AttemptStatusOK, HTTPStatus: ptrInt(resp.StatusCode), LatencyMs: &ttfb,
		})
		e.observeRouting(&loop.routing, item.ID)
		return StreamResult{
			Response:     resp,
			AccountID:    item.ID,
			Provider:     loop.resultProvider(item),
			AttemptCount: i + 1,
			TTFBMs:       ttfb,
		}, nil
	}
	if loop.lastErr == nil {
		loop.lastErr = fmt.Errorf("no worker accounts available")
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
