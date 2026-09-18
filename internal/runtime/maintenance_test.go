package runtime

import (
	"testing"
	"time"
)

func TestNextWorkBuddyFireKinds(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	accounts := []Account{{
		Provider: "workbuddy", Enabled: true, WorkBuddyAutoCheckin: true, WorkBuddyCheckinTime: "08:30",
	}}
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, loc)
	delay, fire := nextWorkBuddyFire(now, accounts)
	if len(fire.checkinTimes) != 1 || fire.checkinTimes[0] != "08:30" {
		t.Fatalf("fire=%+v", fire)
	}
	if delay <= 0 || delay > 45*time.Minute {
		t.Fatalf("delay=%v", delay)
	}
	evening := time.Date(2026, 8, 30, 21, 30, 0, 0, loc)
	_, fire = nextWorkBuddyFire(evening, accounts)
	if !fire.keepalive {
		t.Fatalf("after 21:30 want keepalive, got %+v", fire)
	}
}

func TestNextWorkBuddyFireCombinesCollidingTasks(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	accounts := []Account{{
		Provider: "workbuddy", Enabled: true, WorkBuddyAutoCheckin: true, WorkBuddyCheckinTime: "22:00",
	}}
	now := time.Date(2026, 8, 30, 21, 30, 0, 0, loc)
	_, fire := nextWorkBuddyFire(now, accounts)
	if !fire.keepalive || len(fire.checkinTimes) != 1 || fire.checkinTimes[0] != "22:00" {
		t.Fatalf("fire=%+v", fire)
	}
}
