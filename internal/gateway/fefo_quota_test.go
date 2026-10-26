package gateway

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

func TestQuotaInvalidationRunsAfterTerminalResponseNotStreamHeaders(t *testing.T) {
	var calls []string
	h := &Handler{AfterAccountUse: func(id, model string) { calls = append(calls, id+":"+model) }}
	finish := func(status, id, publicModel string) {
		h.finishRequestLog("", time.Now(), translate.ChatRequest{Model: "paid"}, publicModel, id, "workbuddy", "", status, 0, nil, nil, 1, "")
	}
	finish(accounts.RequestStatusStarted, "synthetic", "paid")
	finish(accounts.RequestStatusStreaming, "synthetic", "paid")
	finish(accounts.RequestStatusOK, "", "paid")
	if len(calls) != 0 {
		t.Fatalf("invalidated before account use finished: %v", calls)
	}
	for _, status := range []string{accounts.RequestStatusOK, accounts.RequestStatusIncomplete, accounts.RequestStatusError, accounts.RequestStatusCanceled} {
		finish(status, "synthetic", "paid")
	}
	finish(accounts.RequestStatusOK, "synthetic", "workbuddy-cn/paid")
	h.finishRequestLog("", time.Now(), translate.ChatRequest{Model: "alias"}, "workbuddy/alias", "synthetic", "workbuddy", "", accounts.RequestStatusOK, 0, nil, nil, 1, "")
	if len(calls) != 6 || calls[0] != "synthetic:paid" || calls[4] != "synthetic:paid" || calls[5] != "synthetic:alias" {
		t.Fatalf("terminal response invalidations = %v", calls)
	}
}
