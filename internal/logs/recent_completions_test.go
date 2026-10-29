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
