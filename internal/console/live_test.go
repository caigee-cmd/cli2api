package console

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

func TestBuildLiveSnapshot(t *testing.T) {
	views := []accounts.AccountView{
		{Account: accounts.Account{ID: "a", Enabled: true, MaxInFlight: 4}, InFlight: 4},
		{Account: accounts.Account{ID: "b", Enabled: true}, InFlight: 1}, // default 4
		{Account: accounts.Account{ID: "c", Enabled: false, MaxInFlight: 8}},
	}
	got := buildLiveSnapshot(views, 17, time.Unix(0, 0))
	if got.InFlight != 5 || got.Capacity != 8 || got.RPM != 17 {
		t.Fatalf("snapshot = %+v", got)
	}
	if len(got.Accounts) != 3 || got.Accounts[1].MaxInFlight != accounts.DefaultMaxInFlight || got.Accounts[2].Enabled {
		t.Fatalf("accounts = %+v", got.Accounts)
	}
}
