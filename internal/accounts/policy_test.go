package accounts

import "testing"

func TestDefaultAccountPolicyValues(t *testing.T) {
	if DefaultMaxInFlightValue(0) != DefaultMaxInFlight || DefaultMaxInFlightValue(-1) != DefaultMaxInFlight {
		t.Fatal("max inflight must default to 4")
	}
	if DefaultMaxInFlightValue(6) != 6 {
		t.Fatal("explicit max inflight must be kept")
	}
	if DefaultPriorityValue(0) != DefaultPriority || DefaultPriorityValue(80) != 80 {
		t.Fatal("priority defaults to 50 unless set")
	}
	if !DefaultDropSystemPrompt("workbuddy", nil) {
		t.Fatal("drop system prompt defaults on")
	}
	if DefaultDropSystemPrompt("trae", nil) {
		t.Fatal("trae must keep caller system prompts by default")
	}
	if DefaultDropSystemPrompt("kimi", nil) {
		t.Fatal("kimi must keep caller system prompts by default")
	}
	if DefaultDropSystemPrompt("codearts", nil) {
		t.Fatal("codearts must keep caller system prompts by default")
	}
	off := false
	if DefaultDropSystemPrompt("workbuddy", &off) {
		t.Fatal("explicit drop system prompt false must stick")
	}
	on := true
	if !DefaultDropSystemPrompt("trae", &on) {
		t.Fatal("explicit trae drop system prompt true must stick")
	}
	if DefaultWorkBuddyAutoCheckin(nil) {
		t.Fatal("auto check-in defaults off")
	}
	if NormalizeAPIKeyMaxInFlight(0) != 0 || NormalizeAPIKeyMaxInFlight(-3) != 0 || NormalizeAPIKeyMaxInFlight(8) != 8 {
		t.Fatal("api key concurrency treats non-positive values as unlimited")
	}
	if err := ValidateAPIKeyMaxInFlight(MaxAPIKeyInFlight); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAPIKeyMaxInFlight(MaxAPIKeyInFlight + 1); err == nil {
		t.Fatal("api key concurrency above the cap must be rejected")
	}
}

func TestResolveWorkBuddyCheckinTimeInheritsDefault(t *testing.T) {
	got, err := ResolveWorkBuddyCheckinTime("", "08:30")
	if err != nil || got != "08:30" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	got, err = ResolveWorkBuddyCheckinTime("10:15", "08:30")
	if err != nil || got != "10:15" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestValidateModelContextLength(t *testing.T) {
	for _, tt := range []struct {
		value int
		ok    bool
	}{
		{-1, false},
		{0, true},
		{512, true},
		{1023, true},
		{1024, true},
		{4000000, true},
		{4000001, false},
	} {
		err := ValidateModelContextLength(tt.value)
		if tt.ok && err != nil {
			t.Fatalf("value=%d err=%v", tt.value, err)
		}
		if !tt.ok && err == nil {
			t.Fatalf("value=%d expected error", tt.value)
		}
	}
}

func TestValidateAccountProxyRejectsQoderSOCKS(t *testing.T) {
	if err := ValidateAccountProxy("qoder", "global", "socks5://proxy.example:1080"); err == nil {
		t.Fatal("qoder socks must fail")
	}
	if err := ValidateAccountProxy("workbuddy", "cn", "socks5://proxy.example:1080"); err != nil {
		t.Fatalf("workbuddy socks: %v", err)
	}
}
