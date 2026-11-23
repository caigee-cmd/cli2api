package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// ProbeAccountResult is the outcome for one model on one account.
type ProbeAccountResult struct {
	AccountID string `json:"account_id"`
	Provider  string `json:"provider,omitempty"`
	Region    string `json:"region,omitempty"`
	OK        bool   `json:"ok"`
	Status    int    `json:"status,omitempty"`
	Code      string `json:"code,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Message   string `json:"message,omitempty"`
	LatencyMS int    `json:"latency_ms"`
}

// ProbeModelResult is the outcome for one model across every currently
// routable account. A probe never starts a request log, changes cooldowns,
// or refreshes the catalog cache.
type ProbeModelResult struct {
	Model        string               `json:"model"`
	OK           bool                 `json:"ok"`
	AccountID    string               `json:"account_id,omitempty"`
	Provider     string               `json:"provider,omitempty"`
	Status       int                  `json:"status,omitempty"`
	Code         string               `json:"code,omitempty"`
	Kind         string               `json:"kind,omitempty"`
	Message      string               `json:"message,omitempty"`
	LatencyMS    int                  `json:"latency_ms"`
	AttemptCount int                  `json:"attempt_count,omitempty"`
	Accounts     []ProbeAccountResult `json:"accounts"`
}

// probeAttempts bounds live upstream calls made by all model probes combined.
// One operator action can check many models across many accounts; without a
// shared cap a single click could open hundreds of upstream requests at once.
const maxConcurrentProbeAttempts = 8

var probeAttemptSem = make(chan struct{}, maxConcurrentProbeAttempts)

// ProbeModel checks publicModel against every currently routable account. It
// reuses normal routing and provider adapters, but caps output at one token so
// a full account/model matrix check stays cheap. Unlike a chat failover loop,
// it never stops after the first working account: the operator needs to know
// which accounts actually serve the model.
func (e ChatExecutor) ProbeModel(ctx context.Context, publicModel string) ProbeModelResult {
	result := ProbeModelResult{Model: publicModel, Accounts: []ProbeAccountResult{}}
	publicModel = strings.TrimSpace(publicModel)
	if publicModel == "" {
		result.Message = "model required"
		return result
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	oneToken := json.RawMessage("1")
	request := translate.ChatRequest{
		Model:               publicModel,
		Messages:            []translate.ChatMessage{{Role: "user", Content: "hi"}},
		Stream:              false,
		MaxTokens:           oneToken,
		MaxCompletionTokens: append(json.RawMessage(nil), oneToken...),
		ReasoningEffort:     json.RawMessage(`"none"`),
		Thinking:            providers.DisabledThinkingFieldJSON(),
	}
	// Console model ids are provider-prefixed when the shared pool is off and
	// bare when it is on. Resolve prefixes while allowing both; explicit
	// region prefixes still pin the route.
	providerFilter, modelRegion := ResolveProviderFilter(&request, true)
	routeModel := request.Model
	regionFilter := regionFilterFromContext(probeCtx)
	if regionFilter == "" {
		regionFilter = modelRegion
	}
	allowed := allowedProvidersFrom(probeCtx)

	items := e.probeAccounts(providerFilter, regionFilter, routeModel, allowed)
	if len(items) == 0 {
		result.Message = "no accounts available"
		return result
	}

	started := time.Now()
	firstOK := -1
	firstFailure := -1
probeAccountsLoop:
	for _, item := range items {
		// If the operator request (or the batch deadline) is gone, stop making
		// new upstream calls instead of marking every remaining account as a
		// client cancellation.
		if err := probeCtx.Err(); err != nil {
			break
		}
		accountResult := ProbeAccountResult{
			AccountID: item.ID,
			Provider:  firstNonEmpty(item.Provider, providerFilter),
			Region:    item.Region,
		}
		if !e.acquireInFlight("", item.ID) {
			accountResult.Code = "account_busy"
			accountResult.Kind = accounts.KindRateLimit
			accountResult.Message = "account is at capacity"
			result.Accounts = append(result.Accounts, accountResult)
			if firstFailure < 0 {
				firstFailure = len(result.Accounts) - 1
			}
			continue
		}
		// Bound the number of simultaneous live calls across all probe batches.
		select {
		case probeAttemptSem <- struct{}{}:
		case <-probeCtx.Done():
			e.releaseInFlight("", item.ID)
			break probeAccountsLoop
		}
		accountStarted := time.Now()
		accountCtx, accountCancel := context.WithTimeout(probeCtx, 15*time.Second)
		probeErr := e.probeModelAttempt(accountCtx, item, request)
		// Capture the deadline state before cancel(): cancel() always sets Err()
		// to Canceled, which would hide a genuine probe-owned timeout.
		accountTimedOut := errors.Is(accountCtx.Err(), context.DeadlineExceeded)
		accountCancel()
		<-probeAttemptSem
		e.releaseInFlight("", item.ID)
		accountResult.LatencyMS = int(time.Since(accountStarted).Milliseconds())
		if probeErr == nil {
			accountResult.OK = true
			accountResult.Status = http.StatusOK
			result.Accounts = append(result.Accounts, accountResult)
			if firstOK < 0 {
				firstOK = len(result.Accounts) - 1
			}
			continue
		}
		// A deadline owned by this probe is an upstream timeout, not a client
		// cancellation. Only a cancelled parent request is a real cancellation.
		if errors.Is(probeErr, context.DeadlineExceeded) && ctx.Err() == nil && accountTimedOut {
			accountResult.Status = http.StatusBadGateway
			accountResult.Code = "probe_timeout"
			accountResult.Kind = accounts.KindUnavailable
			accountResult.Message = "upstream probe timed out"
		} else {
			classified := classifyProbeError(probeErr)
			accountResult.Status = classified.Status
			accountResult.Code = classified.Code
			accountResult.Kind = classified.Kind
			accountResult.Message = classified.Message
		}
		result.Accounts = append(result.Accounts, accountResult)
		if firstFailure < 0 {
			firstFailure = len(result.Accounts) - 1
		}
	}

	result.AttemptCount = len(result.Accounts)
	result.LatencyMS = int(time.Since(started).Milliseconds())
	if firstOK >= 0 {
		okAccount := result.Accounts[firstOK]
		result.OK = true
		result.AccountID = okAccount.AccountID
		result.Provider = okAccount.Provider
		result.LatencyMS = okAccount.LatencyMS
		return result
	}
	if firstFailure >= 0 {
		failed := result.Accounts[firstFailure]
		result.Status = failed.Status
		result.Code = failed.Code
		result.Kind = failed.Kind
		result.Message = fmt.Sprintf("all %d account checks failed; first error: %s", len(result.Accounts), failed.Message)
	}
	return result
}

// probeAccounts enumerates every currently routable account for the model.
// It uses a read-only Pool snapshot so probing never advances rotation cursors,
// region latches, or weight counters used by live traffic.
func (e ChatExecutor) probeAccounts(providerFilter, regionFilter, publicModel string, allowed []string) []Item {
	if e.Pool == nil {
		return nil
	}
	q := RouteQuery{
		PublicModel:      publicModel,
		ProviderFilter:   providerFilter,
		RegionFilter:     regionFilter,
		AllowedProviders: allowed,
	}
	items := e.Pool.RoutableItems(q)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Provider != items[j].Provider {
			return items[i].Provider < items[j].Provider
		}
		return items[i].ID < items[j].ID
	})
	return items
}

func (e ChatExecutor) probeModelAttempt(ctx context.Context, item Item, request translate.ChatRequest) error {
	if isInProcessItem(item) || e.qoderDirect(item) {
		if e.Providers == nil {
			return fmt.Errorf("provider registry unavailable")
		}
		adapter, ok := e.Providers.Get(itemProvider(item))
		if !ok || adapter.Chat == nil {
			return fmt.Errorf("provider %s does not implement chat", itemProvider(item))
		}
		_, err := adapter.Chat.ChatNonStream(ctx, item.ID, sanitizeForItem(item, request))
		return err
	}

	payload, err := json.Marshal(buildWorkerChatPayload(request, false))
	if err != nil {
		return err
	}
	httpReq, err := e.newWorkerRequest(ctx, item, payload, item.ID)
	if err != nil {
		return err
	}
	client := e.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return NewExecutionError(Classified{
			Kind: accounts.KindUnavailable, Status: http.StatusBadGateway,
			Code: "probe_request_failed", Message: err.Error(), Failover: true,
		}, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxWorkerResponseBytes))
	if resp.StatusCode >= 300 {
		classified := classifyWorkerErr(resp, string(body))
		return NewExecutionError(classified, nil)
	}
	if _, err := decodeChatResult(request, bytes.TrimSpace(body)); err != nil {
		return NewExecutionError(Classified{
			Kind: accounts.KindUnavailable, Status: http.StatusBadGateway,
			Code: "probe_decode_failed", Message: err.Error(), Failover: true,
		}, nil)
	}
	return nil
}

func classifyProbeError(err error) Classified {
	classified := ClassifyError(err)
	if classified.Kind == "" {
		classified.Kind = accounts.KindUnavailable
	}
	if classified.Status == 0 {
		classified.Status = http.StatusBadGateway
	}
	if classified.Code == "" {
		classified.Code = classified.Kind
	}
	if classified.Message == "" && err != nil {
		classified.Message = err.Error()
	}
	return classified
}
