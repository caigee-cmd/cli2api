package runtime

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

type fefoRefreshProber struct {
	calls   atomic.Int32
	started chan int32
	release chan struct{}
}

func (p *fefoRefreshProber) Probe(context.Context, string) (providers.AccountHealth, error) {
	return providers.AccountHealth{Ready: true}, nil
}

func (p *fefoRefreshProber) Quota(ctx context.Context, _ string) (*providers.QuotaInfo, error) {
	n := p.calls.Add(1)
	p.started <- n
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return &providers.QuotaInfo{
		ProviderID: "workbuddy", Total: 100, Used: 100, Exceeded: true,
		FetchedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Packages:  []providers.QuotaPackage{{Size: 100, Used: 100, Unit: "credits", Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: time.Now().Add(time.Hour).Unix()}},
	}, nil
}

func newFEFORefreshFixture(t *testing.T) (*Manager, string, *fefoRefreshProber) {
	t.Helper()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "after-use.db"))
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(ManagerConfig{}, store, nil)
	t.Cleanup(func() { _ = m.Close(); _ = store.Close() })
	account, err := store.Create(context.Background(), accounts.CreateAccount{Name: "synthetic-after-use", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	m.pool.SetRoutingStrategy(accounts.RoutingStrategyFEFO)
	m.pool.Upsert(Item{
		ID: account.ID, Provider: "workbuddy", Region: "cn", Models: []string{"paid", "free"}, FreeModels: []string{"free"},
		Quota: &accounts.QuotaSnapshot{Remaining: 10, Total: 100, Packages: []accounts.QuotaPackage{{
			Remain: 10, Size: 100, Unit: "credits", Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: time.Now().Add(time.Hour).Unix(),
		}}},
	})
	p := &fefoRefreshProber{started: make(chan int32, 10), release: make(chan struct{}, 10)}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Prober: p})
	m.SetProviders(registry)
	return m, account.ID, p
}

func awaitFEFORefresh(t *testing.T, p *fefoRefreshProber, want int32) {
	t.Helper()
	select {
	case got := <-p.started:
		if got != want {
			t.Fatalf("refresh number = %d, want %d", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("quota refresh did not start")
	}
}

func TestFEFOCompletionRefreshCoalescesAndReorders(t *testing.T) {
	m, id, p := newFEFORefreshFixture(t)
	m.pool.Upsert(Item{
		ID: "later", Provider: "workbuddy", Region: "cn", Models: []string{"paid"},
		Quota: &accounts.QuotaSnapshot{Remaining: 10, Packages: []accounts.QuotaPackage{{
			Remain: 10, Unit: "credits", Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: time.Now().Add(48 * time.Hour).Unix(),
		}}},
	})
	m.RefreshFEFOQuotaAfterUse(id, "paid")
	awaitFEFORefresh(t, p, 1)
	for i := 0; i < 100; i++ {
		m.RefreshFEFOQuotaAfterUse(id, "paid")
	}
	p.release <- struct{}{}
	awaitFEFORefresh(t, p, 2)
	p.release <- struct{}{}
	m.quotaAfterUseDone.Wait()
	if got := p.calls.Load(); got != 2 {
		t.Fatalf("concurrent invalidations made %d requests, want 2", got)
	}
	item, ok := m.pool.PickRoute(executor.RouteQuery{PublicModel: "paid"})
	if !ok || item.ID != "later" {
		t.Fatalf("spent package remains preferred: %q, %v", item.ID, ok)
	}
	stored, err := m.store.Get(context.Background(), id)
	if err != nil || stored.Quota == nil || !stored.Quota.Exceeded {
		t.Fatalf("new quota not persisted: %v, %+v", err, stored.Quota)
	}
}

func TestFEFOCompletionRefreshSkipsHistoricalStrategiesAndFreeRoutes(t *testing.T) {
	m, id, p := newFEFORefreshFixture(t)
	for _, strategy := range []string{accounts.RoutingStrategyRoundRobin, accounts.RoutingStrategyWeightedRoundRobin, accounts.RoutingStrategyFillFirst} {
		m.pool.SetRoutingStrategy(strategy)
		m.RefreshFEFOQuotaAfterUse(id, "paid")
	}
	m.pool.SetRoutingStrategy(accounts.RoutingStrategyFEFO)
	m.RefreshFEFOQuotaAfterUse(id, "free")
	m.RefreshFEFOQuotaAfterUse("missing", "paid")
	m.quotaAfterUseDone.Wait()
	if p.calls.Load() != 0 {
		t.Fatal("unsupported route created extra quota traffic")
	}
}

func TestFEFOCompletionRefreshStopsOnClose(t *testing.T) {
	m, id, p := newFEFORefreshFixture(t)
	m.RefreshFEFOQuotaAfterUse(id, "paid")
	awaitFEFORefresh(t, p, 1)
	done := make(chan struct{})
	go func() {
		_ = m.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the pending quota fetch")
	}
	m.RefreshFEFOQuotaAfterUse(id, "paid")
	if p.calls.Load() != 1 {
		t.Fatal("closed manager started another quota fetch")
	}
}

type orderedQuotaProber struct {
	started chan int
	release chan struct{}
	calls   atomic.Int32
}

func (p *orderedQuotaProber) Probe(context.Context, string) (providers.AccountHealth, error) {
	return providers.AccountHealth{Ready: true}, nil
}

func (p *orderedQuotaProber) Quota(ctx context.Context, _ string) (*providers.QuotaInfo, error) {
	n := p.calls.Add(1)
	p.started <- int(n)
	if n == 1 {
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	remain := 10.0
	exceeded := false
	if n > 1 {
		remain = 0
		exceeded = true
	}
	return &providers.QuotaInfo{
		ProviderID: "workbuddy", Total: 100, Remaining: remain, Used: 100 - remain, Exceeded: exceeded,
		FetchedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Packages:  []providers.QuotaPackage{{Remain: remain, Size: 100, Unit: "credits", Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: time.Now().Add(time.Hour).Unix()}},
	}, nil
}

// A slow ordinary refresh must not persist an old positive snapshot after the
// post-use refresh has already discovered the spent balance.
func TestQuotaFetchGateSerializesOrdinaryAndAfterUseRefreshes(t *testing.T) {
	m, id, _ := newFEFORefreshFixture(t)
	p := &orderedQuotaProber{started: make(chan int, 10), release: make(chan struct{})}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Prober: p})
	m.SetProviders(registry)
	go func() {
		item, _ := m.pool.ByID(id)
		m.refreshQuotaOnly(context.Background(), item)
	}()
	if got := <-p.started; got != 1 {
		t.Fatalf("first start = %d", got)
	}
	m.RefreshFEFOQuotaAfterUse(id, "paid")
	close(p.release)
	select {
	case got := <-p.started:
		if got != 2 {
			t.Fatalf("second start = %d", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("after-use refresh did not wait for the in-flight ordinary refresh")
	}
	m.quotaAfterUseDone.Wait()
	item, _ := m.pool.ByID(id)
	if item.Quota == nil || !item.Quota.Exceeded {
		t.Fatalf("stale positive snapshot overwrote the newer depleted one: %+v", item.Quota)
	}
}

// An account whose trusted packages expired naturally has Exceeded=false but is
// invisible to FEFO. The stale sweep must still probe it so a new upstream
// package can restore routing, with the normal backoff.
func TestFEFOExpiredTrustedPackagesEnterStaleRevalidation(t *testing.T) {
	m, id, _ := newFEFORefreshFixture(t)
	expired := &accounts.QuotaSnapshot{
		Used: 100, Total: 100, Remaining: 100, Exceeded: false,
		FetchedAt: time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		Packages:  []accounts.QuotaPackage{{Remain: 100, Size: 100, Unit: "credits", Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: time.Now().Add(-time.Minute).Unix()}},
	}
	m.pool.MergeQuota(id, expired)
	item, _ := m.pool.ByID(id)
	if !m.quotaNeedsRevalidationFor(item, time.Now()) {
		t.Fatal("exceeded=false expired trusted packages must be revalidated")
	}
	m.pool.SetRoutingStrategy(accounts.RoutingStrategyRoundRobin)
	if m.quotaNeedsRevalidationFor(item, time.Now()) {
		t.Fatal("stale revalidation must not add traffic outside FEFO")
	}
	m.pool.SetRoutingStrategy(accounts.RoutingStrategyFEFO)
	m.pool.MergeQuota(id, &accounts.QuotaSnapshot{Exceeded: false, Packages: []accounts.QuotaPackage{{Remain: 100, Scope: "unknown", EndsAt: time.Now().Add(-time.Minute).Unix()}}})
	item, _ = m.pool.ByID(id)
	if m.quotaNeedsRevalidationFor(item, time.Now()) {
		t.Fatal("unknown scope must never be treated as depleted")
	}
	m.pool.MergeQuota(id, &accounts.QuotaSnapshot{Exceeded: false, Packages: []accounts.QuotaPackage{{Remain: 100, Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: 0}}})
	item, _ = m.pool.ByID(id)
	if m.quotaNeedsRevalidationFor(item, time.Now()) {
		t.Fatal("unknown expiry must never be treated as depleted")
	}
}

type fefoQuotaFixture struct{ info *providers.QuotaInfo }

func (f fefoQuotaFixture) Probe(context.Context, string) (providers.AccountHealth, error) {
	return providers.AccountHealth{Ready: true}, nil
}

func (f fefoQuotaFixture) Quota(context.Context, string) (*providers.QuotaInfo, error) {
	return f.info, nil
}

func TestFetchProviderQuotaPreservesPackageScopeOnlyForWorkBuddy(t *testing.T) {
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "quota-fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	account, err := store.Create(ctx, accounts.CreateAccount{Name: "scope-fixture", Provider: "workbuddy", Region: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	manager := &Manager{pool: NewPool(nil, nil), store: store}
	manager.pool.Upsert(Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})
	for _, providerID := range []string{"workbuddy", "trae", ""} {
		t.Run(providerID, func(t *testing.T) {
			info := &providers.QuotaInfo{
				ProviderID: providerID, Remaining: 10, Total: 10, FetchedAt: "2026-10-24T00:00:00Z",
				ExpiresAt: 1792857600, ExpiringRemain: 10,
				Packages: []providers.QuotaPackage{
					{Remain: 10, Size: 10, Scope: accounts.QuotaPackageScopeAccountFEFO, EndsAt: 1792857600, EndTime: "2026-10-25 00:00:00"},
					{Remain: 5, Size: 5, Unit: "credits", Scope: "work-only"},
					{Remain: 5, Size: 5},
				},
			}
			manager.fetchProviderQuota(ctx, account.ID, fefoQuotaFixture{info: info})
			item, _ := manager.pool.ByID(account.ID)
			stored, err := store.Get(ctx, account.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, quota := range []*accounts.QuotaSnapshot{item.Quota, stored.Quota} {
				if quota == nil {
					t.Fatal("missing quota snapshot")
				}
				if providerID != "workbuddy" {
					if len(quota.Packages) != 0 || quota.ExpiresAt != 0 || quota.ExpiringRemain != 0 {
						t.Fatalf("non-WorkBuddy package metadata leaked through gate: %+v", quota)
					}
					continue
				}
				if len(quota.Packages) != 3 || quota.Packages[0].Scope != accounts.QuotaPackageScopeAccountFEFO ||
					quota.Packages[1].Scope != "work-only" || quota.Packages[2].Scope != "" ||
					quota.Packages[0].Unit != "credits" || quota.Packages[0].EndTime != "2026-10-25 00:00:00" ||
					quota.ExpiresAt != info.ExpiresAt || quota.ExpiringRemain != 10 {
					t.Fatalf("package facts were not preserved: %+v", quota)
				}
			}
		})
	}
}
