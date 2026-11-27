package providers

import (
	"testing"
	"time"
)

func TestCheckinPoliciesUseProviderServerClock(t *testing.T) {
	for _, descriptor := range List() {
		for _, region := range descriptor.Regions {
			if region.Checkin == nil {
				continue
			}
			if region.Checkin.Timezone == "" || region.Checkin.Timezone == "Local" {
				t.Fatalf("%s/%s check-in must use an explicit server timezone, got %q", descriptor.ID, region.ID, region.Checkin.Timezone)
			}
			if _, err := time.LoadLocation(region.Checkin.Timezone); err != nil {
				t.Fatalf("%s/%s invalid server timezone %q: %v", descriptor.ID, region.ID, region.Checkin.Timezone, err)
			}
		}
	}
}

func TestListIncludesEveryRegisteredDescriptor(t *testing.T) {
	listed := List()
	seen := make(map[string]struct{}, len(listed))
	for _, descriptor := range listed {
		if _, duplicate := seen[descriptor.ID]; duplicate {
			t.Fatalf("duplicate provider %q", descriptor.ID)
		}
		seen[descriptor.ID] = struct{}{}
	}
	for id := range registry {
		if _, ok := seen[id]; !ok {
			t.Fatalf("provider %q is registered but missing from List", id)
		}
	}
	codex, ok := Get("codex")
	if !ok {
		t.Fatal("codex descriptor missing")
	}
	if codex.Runtime != RuntimeInProcess || codex.DefaultRegion != "global" || !codex.SupportsAuthType(AuthOAuth) {
		t.Fatalf("unexpected codex descriptor: %+v", codex)
	}
}

func TestQwenWorkAdvertisesDailyCheckin(t *testing.T) {
	for _, region := range []string{"global", "cn"} {
		policy, ok := CheckinFor("qwenwork", region)
		if !ok || policy == nil {
			t.Fatalf("region %s: qwenwork check-in not advertised", region)
		}
		if policy.Timezone != "Asia/Shanghai" {
			t.Fatalf("region %s: timezone=%q", region, policy.Timezone)
		}
	}
	descriptor, ok := Get("qwenwork")
	if !ok || !descriptor.SupportsCheckin() {
		t.Fatal("qwenwork descriptor must support check-in")
	}
}

func TestMiniMaxCodeAdvertisesDailyCheckin(t *testing.T) {
	for _, region := range []string{"cn", "global"} {
		policy, ok := CheckinFor("minimaxcode", region)
		if !ok || policy == nil {
			t.Fatalf("region %s: minimaxcode check-in not advertised", region)
		}
		if policy.Timezone != "Asia/Shanghai" {
			t.Fatalf("region %s: timezone=%q", region, policy.Timezone)
		}
	}
	descriptor, ok := Get("minimaxcode")
	if !ok || !descriptor.SupportsCheckin() {
		t.Fatal("minimaxcode descriptor must support check-in")
	}
}
