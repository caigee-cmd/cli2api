package logs

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestDrainRecentCompletions(t *testing.T) {
	r := NewRequestRecorder(persistOnly{})
	defer r.Close()

	r.Finish(accounts.RequestLog{AccountID: "a1", Status: accounts.RequestStatusOK})
	r.Finish(accounts.RequestLog{AccountID: "a2", Status: accounts.RequestStatusError})
	r.Finish(accounts.RequestLog{AccountID: "a3", Status: accounts.RequestStatusStreaming}) // ignored
	r.Finish(accounts.RequestLog{AccountID: "", Status: accounts.RequestStatusError})         // ignored

	got := r.DrainRecentCompletions()
	if len(got) != 2 {
		t.Fatalf("want 2 completions, got %+v", got)
	}
	if got[0].AccountID != "a1" || got[0].Status != accounts.RequestStatusOK {
		t.Fatalf("got[0]=%+v", got[0])
	}
	if got[1].AccountID != "a2" || got[1].Status != accounts.RequestStatusError {
		t.Fatalf("got[1]=%+v", got[1])
	}
	if again := r.DrainRecentCompletions(); len(again) != 0 {
		t.Fatalf("second drain must be empty, got %+v", again)
	}
}

func TestSelectingRequestsFIFOAndBind(t *testing.T) {
	r := NewRequestRecorder(persistOnly{})
	defer r.Close()

	// Started well past selectingMinDwell so Bind drops from projection immediately.
	past := time.Now().UTC().Add(-(selectingMinDwell + time.Second))
	r.Start(accounts.RequestLog{ID: "r1", RequestedModel: "m1", Status: accounts.RequestStatusStarted, CreatedAt: past})
	r.Start(accounts.RequestLog{ID: "r2", RequestedModel: "m2", Status: accounts.RequestStatusStarted, CreatedAt: past.Add(time.Millisecond)})
	r.Start(accounts.RequestLog{ID: "r3", RequestedModel: "m3", Status: accounts.RequestStatusStarted, CreatedAt: past.Add(2 * time.Millisecond)})

	got := r.SelectingRequests()
	if len(got) != 3 {
		t.Fatalf("want 3 selecting, got %+v", got)
	}
	if got[0].ID != "r1" || got[1].ID != "r2" || got[2].ID != "r3" {
		t.Fatalf("FIFO order = %+v", got)
	}

	r.BindAccount("r2", "acc-b")
	got = r.SelectingRequests()
	if len(got) != 2 || got[0].ID != "r1" || got[1].ID != "r3" {
		t.Fatalf("after bind r2 = %+v", got)
	}

	r.UnbindAccount("r2", "acc-b")
	got = r.SelectingRequests()
	if len(got) != 3 {
		t.Fatalf("after unbind want 3, got %+v", got)
	}

	r.Finish(accounts.RequestLog{ID: "r1", Status: accounts.RequestStatusStreaming, AccountID: "acc-a"})
	got = r.SelectingRequests()
	// streaming is non-terminal — still open; AccountID on Finish does not Bind.
	// r1 still selecting (empty AccountID on open map unless Bind was called).
	ids := map[string]bool{}
	for _, o := range got {
		ids[o.ID] = true
	}
	if !ids["r1"] || !ids["r2"] || !ids["r3"] {
		t.Fatalf("streaming finish must not clearOpen, got %+v", got)
	}

	r.Finish(accounts.RequestLog{ID: "r1", Status: accounts.RequestStatusOK, AccountID: "acc-a"})
	got = r.SelectingRequests()
	ids = map[string]bool{}
	for _, o := range got {
		ids[o.ID] = true
	}
	if ids["r1"] || !ids["r2"] || !ids["r3"] {
		t.Fatalf("terminal ok must clear r1, got %+v", got)
	}
}

func TestSelectingMinDwellDisplayOnly(t *testing.T) {
	r := NewRequestRecorder(persistOnly{})
	defer r.Close()

	now := time.Now().UTC()
	r.Start(accounts.RequestLog{
		ID: "fresh", RequestedModel: "m", Status: accounts.RequestStatusStarted, CreatedAt: now,
	})
	r.BindAccount("fresh", "acc-x")
	got := r.SelectingRequests()
	if len(got) != 1 || got[0].ID != "fresh" || got[0].AccountID != "acc-x" {
		t.Fatalf("bound but within dwell must still project as selecting with AccountID, got %+v", got)
	}

	old := now.Add(-(selectingMinDwell + time.Millisecond))
	r.Start(accounts.RequestLog{
		ID: "aged", RequestedModel: "m", Status: accounts.RequestStatusStarted, CreatedAt: old,
	})
	r.BindAccount("aged", "acc-y")
	got = r.SelectingRequests()
	ids := map[string]bool{}
	for _, o := range got {
		ids[o.ID] = true
	}
	if !ids["fresh"] {
		t.Fatalf("fresh still in dwell missing: %+v", got)
	}
	if ids["aged"] {
		t.Fatalf("aged past dwell must leave selecting projection: %+v", got)
	}
	if got[0].AccountID != "acc-x" {
		t.Fatalf("fresh must keep bind target for fly handoff, got %+v", got[0])
	}
}

func TestBoundRequestsPerAccountElapsed(t *testing.T) {
	r := NewRequestRecorder(persistOnly{})
	defer r.Close()

	past := time.Now().UTC().Add(-(selectingMinDwell + time.Second))
	r.Start(accounts.RequestLog{ID: "a1", RequestedModel: "sonnet", Status: accounts.RequestStatusStarted, CreatedAt: past})
	r.Start(accounts.RequestLog{ID: "a2", RequestedModel: "gpt", Status: accounts.RequestStatusStarted, CreatedAt: past.Add(time.Millisecond)})
	r.Start(accounts.RequestLog{ID: "q1", RequestedModel: "wait", Status: accounts.RequestStatusStarted, CreatedAt: past.Add(2 * time.Millisecond)})

	if got := r.BoundRequests(); len(got) != 0 {
		t.Fatalf("unbound must be empty, got %+v", got)
	}

	r.BindAccount("a2", "acc-x")
	time.Sleep(2 * time.Millisecond)
	r.BindAccount("a1", "acc-x")
	got := r.BoundRequests()
	if len(got) != 2 {
		t.Fatalf("want 2 bound, got %+v", got)
	}
	// FIFO by BoundAt: a2 bound first.
	if got[0].ID != "a2" || got[1].ID != "a1" {
		t.Fatalf("bound order = %+v", got)
	}
	if got[0].AccountID != "acc-x" || got[0].RequestedModel != "gpt" {
		t.Fatalf("got[0]=%+v", got[0])
	}
	if got[0].BoundAt.IsZero() || got[1].BoundAt.IsZero() {
		t.Fatalf("bound must stamp BoundAt: %+v", got)
	}

	r.UnbindAccount("a2", "acc-x")
	got = r.BoundRequests()
	if len(got) != 1 || got[0].ID != "a1" {
		t.Fatalf("after unbind a2 = %+v", got)
	}
	if !got[0].BoundAt.IsZero() {
		// a1 still bound — BoundAt must remain
	} else {
		t.Fatalf("remaining bound a1 lost BoundAt")
	}
}
