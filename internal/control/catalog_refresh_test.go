package control

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCatalogStore struct {
	mu   sync.Mutex
	data map[string]string
	err  error
}

func (s *fakeCatalogStore) GetSecret(_ context.Context, name string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", false, s.err
	}
	value, ok := s.data[name]
	return value, ok, nil
}

func (s *fakeCatalogStore) SetSecret(_ context.Context, name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.data == nil {
		s.data = map[string]string{}
	}
	s.data[name] = value
	return nil
}

func TestCatalogHealthTracksPerProviderOutcome(t *testing.T) {
	now := time.Date(2026, 10, 26, 3, 0, 0, 0, time.UTC)
	health := NewCatalogHealth()
	health.now = func() time.Time { return now }
	health.Attempt("qoder")
	health.Success("qoder", 14)
	health.Attempt("workbuddy")
	health.Failure("workbuddy", errors.New("upstream 503"))

	rows := health.Snapshot()
	if len(rows) != 2 {
		t.Fatalf("rows=%v", rows)
	}
	if rows[0].Provider != "qoder" || rows[0].Models != 14 || rows[0].Stale || rows[0].LastError != "" {
		t.Fatalf("qoder row=%+v", rows[0])
	}
	if rows[0].LastSuccessAt != now.Format(time.RFC3339) {
		t.Fatalf("qoder success at=%q", rows[0].LastSuccessAt)
	}
	if rows[1].Provider != "workbuddy" || !rows[1].Stale || rows[1].LastError != "upstream 503" {
		t.Fatalf("workbuddy row=%+v", rows[1])
	}

	// A later success clears the failure without dropping the provider.
	health.Success("workbuddy", 26)
	rows = health.Snapshot()
	if rows[1].Stale || rows[1].LastError != "" || rows[1].Models != 26 {
		t.Fatalf("workbuddy after success=%+v", rows[1])
	}
}

func TestCatalogPersistsAndRestoresSnapshot(t *testing.T) {
	store := &fakeCatalogStore{}
	first := NewCatalog(func(bool, string, CatalogMode) ([]map[string]any, error) {
		return []map[string]any{{"id": "glm-5.3", "provider": "workbuddy"}}, nil
	})
	first.Store = store
	first.Health = NewCatalogHealth()
	first.Health.Success("workbuddy", 1)

	if _, err := first.Get(true, "", CatalogModeMerge); err != nil {
		t.Fatal(err)
	}
	if store.data[catalogSnapshotSecret] == "" {
		t.Fatal("snapshot was not persisted")
	}

	// A restart starts with an upstream that fails; the stored snapshot must
	// still answer /v1/models.
	restored := NewCatalog(func(bool, string, CatalogMode) ([]map[string]any, error) {
		return nil, errors.New("upstream unreachable")
	})
	restored.Store = store
	restored.Health = NewCatalogHealth()
	restored.Restore(context.Background())

	models, err := restored.Get(false, "", CatalogModeMerge)
	if err != nil {
		t.Fatalf("restored read failed: %v", err)
	}
	if len(models) != 1 || models[0]["id"] != "glm-5.3" {
		t.Fatalf("restored models=%v", models)
	}
	rows := restored.Status().Providers
	if len(rows) != 1 || rows[0].Provider != "workbuddy" {
		t.Fatalf("restored health=%v", rows)
	}
}

func TestCatalogServesLastKnownCatalogAfterFailure(t *testing.T) {
	var fail atomic.Bool
	catalog := NewCatalog(func(bool, string, CatalogMode) ([]map[string]any, error) {
		if fail.Load() {
			return nil, errors.New("upstream 502")
		}
		return []map[string]any{{"id": "kimi-k3"}}, nil
	})
	if _, err := catalog.Get(true, "", CatalogModeMerge); err != nil {
		t.Fatal(err)
	}
	fail.Store(true)

	models, err := catalog.Get(true, "", CatalogModeMerge)
	if err != nil {
		t.Fatalf("failed refresh must not fail a served catalog: %v", err)
	}
	if len(models) != 1 || models[0]["id"] != "kimi-k3" {
		t.Fatalf("models after failure=%v", models)
	}
	status := catalog.Status()
	if !status.Stale || status.LastError != "upstream 502" {
		t.Fatalf("status=%+v", status)
	}
	if status.SyncedAt == "" {
		t.Fatal("synced_at must survive a failure")
	}
}

func TestCatalogRefreshReportsStatus(t *testing.T) {
	var calls atomic.Int32
	catalog := NewCatalog(func(bool, string, CatalogMode) ([]map[string]any, error) {
		calls.Add(1)
		return []map[string]any{{"id": "sonus"}}, nil
	})
	catalog.Interval = 10 * time.Minute

	status, err := catalog.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	if status.SyncedAt == "" || status.Stale || status.Refreshing {
		t.Fatalf("status=%+v", status)
	}
	if status.IntervalSeconds != 600 {
		t.Fatalf("interval=%d", status.IntervalSeconds)
	}
}

func TestCatalogRefresherTicks(t *testing.T) {
	var calls atomic.Int32
	catalog := NewCatalog(func(bool, string, CatalogMode) ([]map[string]any, error) {
		calls.Add(1)
		return []map[string]any{{"id": "cantus"}}, nil
	})
	catalog.Interval = 20 * time.Millisecond

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		catalog.RunRefresher(stop)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if calls.Load() >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(stop)
	<-done
	if calls.Load() < 2 {
		t.Fatalf("refresher did not tick: calls=%d", calls.Load())
	}
}

func TestCatalogRefresherDisabledWithZeroInterval(t *testing.T) {
	var calls atomic.Int32
	catalog := NewCatalog(func(bool, string, CatalogMode) ([]map[string]any, error) {
		calls.Add(1)
		return nil, nil
	})
	catalog.Interval = 0
	stop := make(chan struct{})
	close(stop)
	catalog.RunRefresher(stop)
	if calls.Load() != 0 {
		t.Fatalf("disabled refresher fetched %d times", calls.Load())
	}
}
