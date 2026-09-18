package runtime

import (
	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type (
	Account          = accounts.Account
	AccountStore     = accounts.AccountStore
	PoolStateStore   = accounts.PoolStateStore
	CreateAccount    = accounts.CreateAccount
	UpdateAccount    = accounts.UpdateAccount
	ImportAccount    = accounts.ImportAccount
	AccountView      = accounts.AccountView
	NativeCredential = accounts.NativeCredential
	Item             = accounts.Item
	Pool             = accounts.Pool
	CooldownRow      = accounts.CooldownRow
	QuotaSnapshot    = accounts.QuotaSnapshot
	CheckinRecord    = accounts.CheckinRecord
)

const (
	KindUnavailable             = accounts.KindUnavailable
	DefaultWorkBuddyCheckinTime = accounts.DefaultWorkBuddyCheckinTime
	backoffMaxLevel             = accounts.BackoffMaxLevel
)

var ErrAccountNotFound = accounts.ErrAccountNotFound

func NewPool(urls, ids []string) *Pool {
	return accounts.NewPool(urls, ids)
}

func NormalizeWeight(priority int) int {
	return accounts.NormalizeWeight(priority)
}

func clampBackoffLevel(level int) int {
	return accounts.ClampBackoffLevel(level)
}

func NormalizeWorkBuddyCheckinTime(value string) (string, error) {
	return accounts.NormalizeWorkBuddyCheckinTime(value)
}
