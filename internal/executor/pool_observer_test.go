package executor

import "testing"

func TestPoolObserverIsDataOnly(t *testing.T) {
	var seen Item
	pool := NewPool(nil, nil)
	pool.SetObserver(func(item Item) { seen = item })
	ready := true
	pool.Upsert(Item{ID: "acc_1", Ready: &ready})
	pool.MarkOK("acc_1", "glm-5")
	if seen.ID != "acc_1" {
		t.Fatalf("observer item = %+v", seen)
	}
}
