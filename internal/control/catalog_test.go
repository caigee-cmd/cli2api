package control

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogCacheTTLAndRefreshDedup(t *testing.T) {
	var hits atomic.Int32
	catalog := NewCatalog(func(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error) {
		hits.Add(1)
		return []map[string]any{{"id": "glm-5.2", "account": accountID, "refresh": refresh, "mode": int(mode)}}, nil
	})
	catalog.TTL = time.Minute

	first, err := catalog.Get(false, "acc", CatalogModeMerge)
	if err != nil || len(first) != 1 {
		t.Fatalf("first=%v err=%v", first, err)
	}
	second, err := catalog.Get(false, "acc", CatalogModeMerge)
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatalf("ttl hits=%d want 1", hits.Load())
	}
	if second[0]["id"] != "glm-5.2" {
		t.Fatalf("cached=%v", second)
	}

	regional, err := catalog.Get(false, "acc", CatalogModeExpand)
	if err != nil || hits.Load() != 2 {
		t.Fatalf("regional hits=%d err=%v models=%v", hits.Load(), err, regional)
	}
	other, err := catalog.Get(false, "", CatalogModeMerge)
	if err != nil || hits.Load() != 3 {
		t.Fatalf("empty-account hits=%d err=%v models=%v", hits.Load(), err, other)
	}
	forced, err := catalog.Get(true, "acc", CatalogModeMerge)
	if err != nil || hits.Load() != 4 {
		t.Fatalf("force hits=%d err=%v models=%v", hits.Load(), err, forced)
	}
	if catalog.CachedCount("acc", CatalogModeMerge) != 1 {
		t.Fatalf("cached count=%d", catalog.CachedCount("acc", CatalogModeMerge))
	}
}

func TestCatalogStaleServesWhileRefreshRuns(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var hits atomic.Int32
	catalog := NewCatalog(func(refresh bool, accountID string, mode CatalogMode) ([]map[string]any, error) {
		n := hits.Add(1)
		if n == 1 {
			return []map[string]any{{"id": "old"}}, nil
		}
		close(started)
		<-release
		return []map[string]any{{"id": "new"}}, nil
	})
	if _, err := catalog.Get(false, "", CatalogModeMerge); err != nil {
		t.Fatal(err)
	}
	catalog.TTL = time.Nanosecond
	time.Sleep(time.Millisecond)
	stale, err := catalog.Get(false, "", CatalogModeMerge)
	if err != nil {
		t.Fatal(err)
	}
	if stale[0]["id"] != "old" {
		t.Fatalf("stale=%v", stale)
	}
	<-started
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fresh := catalog.snapshot("", CatalogModeMerge)
		if len(fresh) == 1 && fresh[0]["id"] == "new" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("background refresh did not land")
}
