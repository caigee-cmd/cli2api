package runtime

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/executor"
	"github.com/caigee-cmd/cli2api/internal/providers"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

// scriptedProber returns a fixed quota snapshot on every Quota() call and
// counts how many times it was asked. The FetchedAt it reports is stamped at
// call time so the merged snapshot looks freshly fetched, exactly like a real
// successful probe.
type scriptedProber struct {
	exceeded bool
	calls    int
	err      error
}

func (p *scriptedProber) Probe(ctx context.Context, accountID string) (providers.AccountHealth, error) {
	return providers.AccountHealth{Ready: true, Hot: true}, nil
}

func (p *scriptedProber) Quota(ctx context.Context, accountID string) (*providers.QuotaInfo, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	used, remain := 100.0, 900.0
	if p.exceeded {
		used, remain = 100.0, 0.0
	}
	return &providers.QuotaInfo{
		Used: used, Total: 100, Remaining: remain, Percentage: used,
		Unit: "credits", Exceeded: p.exceeded,
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func newQuotaBackoffTestManager(t *testing.T, prober *scriptedProber) (*Manager, string) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "qoder.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	account, err := store.Create(ctx, accounts.CreateAccount{
		Name: "probe", Provider: "workbuddy", Region: "cn", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	registry := providers.NewRegistry()
	registry.Register(providers.Adapter{ID: "workbuddy", Prober: prober})
	manager := NewManager(ManagerConfig{DataDir: t.TempDir()}, store, &noopStarter{})
	manager.SetProviders(registry)
	manager.Pool().Upsert(executor.Item{ID: account.ID, Provider: "workbuddy", Runtime: "in_process"})
	return manager, account.ID
}

// agedExceeded returns an Exceeded snapshot whose FetchedAt is age old, so the
// test can drive the sweep's staleness check without sleeping.
func agedExceeded(age time.Duration) *accounts.QuotaSnapshot {
	return &accounts.QuotaSnapshot{
		Used: 100, Total: 100, Remaining: 0, Percentage: 100,
		Unit: "credits", Exceeded: true,
		FetchedAt: time.Now().UTC().Add(-age).Format(time.RFC3339),
	}
}

func TestRefreshStaleQuotaBacksOffExhaustedAccount(t *testing.T) {
	prober := &scriptedProber{exceeded: true}
	manager, id := newQuotaBackoffTestManager(t, prober)
	ctx := context.Background()

	// Levels 0..2 all run at a 5-minute delay. A 10-minute-old snapshot is
	// therefore probed on each of the first three sweeps, and each successful
	// but still-exhausted probe bumps the level.
	for i := 0; i < 3; i++ {
		manager.Pool().MergeQuota(id, agedExceeded(10*time.Minute))
		manager.refreshStaleQuota(ctx)
		if prober.calls != i+1 {
			t.Fatalf("sweep %d: calls = %d, want %d", i, prober.calls, i+1)
		}
		if got := manager.quotaProbeLevel(id); got != i+1 {
			t.Fatalf("sweep %d: level = %d, want %d", i, got, i+1)
		}
	}

	// Level 3 raises the delay to 15 minutes: a 10-minute-old snapshot must
	// now be skipped instead of hammered.
	manager.Pool().MergeQuota(id, agedExceeded(10*time.Minute))
	manager.refreshStaleQuota(ctx)
	if prober.calls != 3 {
		t.Fatalf("fresh-enough snapshot was probed: calls = %d, want 3", prober.calls)
	}
	if got := manager.quotaProbeLevel(id); got != 3 {
		t.Fatalf("level changed while skipping: %d, want 3", got)
	}

	// Once the snapshot is older than the level-3 delay it is probed again.
	manager.Pool().MergeQuota(id, agedExceeded(20*time.Minute))
	manager.refreshStaleQuota(ctx)
	if prober.calls != 4 {
		t.Fatalf("stale snapshot was not probed: calls = %d, want 4", prober.calls)
	}
	if got := manager.quotaProbeLevel(id); got != 4 {
		t.Fatalf("level = %d, want 4", got)
	}

	// Level 4 is a 30-minute delay; a 20-minute-old snapshot is skipped.
	manager.Pool().MergeQuota(id, agedExceeded(20*time.Minute))
	manager.refreshStaleQuota(ctx)
	if prober.calls != 4 {
		t.Fatalf("level-4 skip probed anyway: calls = %d, want 4", prober.calls)
	}

	// A recovered balance resets the ladder to the fast end.
	prober.exceeded = false
	manager.Pool().MergeQuota(id, agedExceeded(31*time.Minute))
	manager.refreshStaleQuota(ctx)
	if prober.calls != 5 {
		t.Fatalf("recovery probe did not run: calls = %d, want 5", prober.calls)
	}
	if got := manager.quotaProbeLevel(id); got != 0 {
		t.Fatalf("level after recovery = %d, want 0", got)
	}
}

func TestRefreshStaleQuotaKeepsLevelWhenProbeFails(t *testing.T) {
	prober := &scriptedProber{exceeded: true, err: context.DeadlineExceeded}
	manager, id := newQuotaBackoffTestManager(t, prober)
	ctx := context.Background()

	manager.Pool().MergeQuota(id, agedExceeded(10*time.Minute))
	manager.refreshStaleQuota(ctx)
	// A failed probe never merges a new snapshot, so FetchedAt is unchanged and
	// the level must not climb: a transient outage should retry at the same
	// cadence rather than escalate to an hours-long wait.
	if got := manager.quotaProbeLevel(id); got != 0 {
		t.Fatalf("level after failed probe = %d, want 0", got)
	}
	// The stale snapshot is retried on the next sweep at the same fast cadence.
	manager.refreshStaleQuota(ctx)
	if prober.calls != 2 {
		t.Fatalf("failed probe was not retried: calls = %d, want 2", prober.calls)
	}
}

type noopStarter struct{}

func (n *noopStarter) Start(context.Context, Account, string, int) (ManagedProcess, error) {
	return nil, nil
}
