package logs

import (
	"testing"

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

	r.Start(accounts.RequestLog{ID: "r1", RequestedModel: "m1", Status: accounts.RequestStatusStarted})
	r.Start(accounts.RequestLog{ID: "r2", RequestedModel: "m2", Status: accounts.RequestStatusStarted})
	r.Start(accounts.RequestLog{ID: "r3", RequestedModel: "m3", Status: accounts.RequestStatusStarted})

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
