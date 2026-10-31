package codex

import (
	"testing"
	"time"
)

func TestRefreshLeadOuterVsSIWC(t *testing.T) {
	outer := Credential{
		AccessToken:  "outer-token",
		RefreshToken: "outer-refresh",
		AccountID:    "acct-1",
		AuthMethod:   AuthMethodCLI,
		ExpiresAt:    time.Now().Add(10 * 24 * time.Hour).Unix(),
	}
	if got := outer.refreshLead(); got != outerRefreshLead {
		t.Fatalf("CLI/outer lead = %v, want %v", got, outerRefreshLead)
	}

	siwc := Credential{
		AccessToken:  "siwc-token",
		RefreshToken: "siwc-refresh",
		AccountID:    "acct-1",
		AuthMethod:   AuthMethodChatGPT,
		ClientID:     "issued-client",
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
	if got := siwc.refreshLead(); got != siwcRefreshLead {
		t.Fatalf("SIWC lead = %v, want %v", got, siwcRefreshLead)
	}

	// Codex-client EffectiveForRequest selects the outer partition (AuthMethodCLI),
	// so the long outer lead applies even on a dual UseSIWC credential.
	dual := dualSIWCCredential()
	effective, err := dual.EffectiveForRequest("codex-rs/0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !effective.refreshingOuter() {
		t.Fatal("expected refreshingOuter on Codex UA dual credential")
	}
	if got := effective.refreshLead(); got != outerRefreshLead {
		t.Fatalf("Codex UA effective lead = %v, want outer %v", got, outerRefreshLead)
	}
	if got := dual.refreshLead(); got != siwcRefreshLead {
		t.Fatalf("dual primary (SIWC) lead = %v, want SIWC %v", got, siwcRefreshLead)
	}
}

func TestNeedsRefreshOuterLead96h(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	// Nominal 10-day JWT with 48h remaining: still inside the 96h outer lead.
	outerSoon := Credential{
		AccessToken:  "tok",
		RefreshToken: "ref",
		AccountID:    "acct",
		AuthMethod:   AuthMethodCLI,
		ExpiresAt:    now.Add(48 * time.Hour).Unix(),
	}
	if !outerSoon.needsRefresh(now) {
		t.Fatal("outer with 48h remaining should refresh under 96h lead")
	}

	// More than 96h remaining: no proactive refresh yet.
	outerFresh := outerSoon
	outerFresh.ExpiresAt = now.Add(100 * time.Hour).Unix()
	if outerFresh.needsRefresh(now) {
		t.Fatal("outer with 100h remaining should not refresh yet")
	}

	// SIWC keeps the short 5m lead: 10m remaining is still fine.
	siwcFresh := Credential{
		AccessToken:  "tok",
		RefreshToken: "ref",
		AccountID:    "acct",
		AuthMethod:   AuthMethodChatGPT,
		ClientID:     "cid",
		ExpiresAt:    now.Add(10 * time.Minute).Unix(),
	}
	if siwcFresh.needsRefresh(now) {
		t.Fatal("SIWC with 10m remaining should not refresh under 5m lead")
	}
	siwcSoon := siwcFresh
	siwcSoon.ExpiresAt = now.Add(3 * time.Minute).Unix()
	if !siwcSoon.needsRefresh(now) {
		t.Fatal("SIWC with 3m remaining should refresh under 5m lead")
	}

	// Same wall-clock remaining as the outerSoon case must NOT trigger SIWC refresh.
	siwc48h := siwcFresh
	siwc48h.ExpiresAt = now.Add(48 * time.Hour).Unix()
	if siwc48h.needsRefresh(now) {
		t.Fatal("SIWC with 48h remaining must not use the outer 96h lead")
	}
}

func TestCatalogIncludesGPT61Sol(t *testing.T) {
	entry, ok := lookupModel("gpt-6.1-sol")
	if !ok {
		t.Fatal("gpt-6.1-sol missing from Codex models.json")
	}
	if entry.DisplayName != "GPT-6.1-Sol" {
		t.Fatalf("display_name = %q", entry.DisplayName)
	}
	if entry.ContextWindow != 272000 || entry.MaxContextWindow != 872000 {
		t.Fatalf("context windows = %d/%d", entry.ContextWindow, entry.MaxContextWindow)
	}
	if entry.DefaultReasoning != "low" {
		t.Fatalf("default_reasoning_level = %q, want low", entry.DefaultReasoning)
	}
	if !entry.UseResponsesLite || !entry.SupportedInAPI || !entry.PreferWebsockets {
		t.Fatalf("flags lite=%v api=%v ws=%v", entry.UseResponsesLite, entry.SupportedInAPI, entry.PreferWebsockets)
	}
	wantLevels := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	if len(entry.ReasoningLevels) != len(wantLevels) {
		t.Fatalf("reasoning levels = %v, want %v", entry.ReasoningLevels, wantLevels)
	}
	for i, level := range wantLevels {
		if entry.ReasoningLevels[i] != level {
			t.Fatalf("reasoning levels = %v, want %v", entry.ReasoningLevels, wantLevels)
		}
	}
}

func TestCatalogIncludesGPT61Terra(t *testing.T) {
	entry, ok := lookupModel("gpt-6.1-terra")
	if !ok {
		t.Fatal("gpt-6.1-terra missing from Codex models.json")
	}
	if entry.DisplayName != "GPT-6.1 Terra" {
		t.Fatalf("display_name = %q", entry.DisplayName)
	}
	if entry.ContextWindow != 272000 || entry.MaxContextWindow != 872000 {
		t.Fatalf("context windows = %d/%d", entry.ContextWindow, entry.MaxContextWindow)
	}
	if entry.DefaultReasoning != "low" {
		t.Fatalf("default_reasoning_level = %q, want low", entry.DefaultReasoning)
	}
	if !entry.UseResponsesLite || !entry.SupportedInAPI || !entry.PreferWebsockets {
		t.Fatalf("flags lite=%v api=%v ws=%v", entry.UseResponsesLite, entry.SupportedInAPI, entry.PreferWebsockets)
	}
	wantLevels := []string{"low", "medium", "high", "xhigh", "max", "ultra"}
	if len(entry.ReasoningLevels) != len(wantLevels) {
		t.Fatalf("reasoning levels = %v, want %v", entry.ReasoningLevels, wantLevels)
	}
	for i, level := range wantLevels {
		if entry.ReasoningLevels[i] != level {
			t.Fatalf("reasoning levels = %v, want %v", entry.ReasoningLevels, wantLevels)
		}
	}
}
