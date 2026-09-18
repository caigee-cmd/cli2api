package control

import "github.com/caigee-cmd/cli2api/internal/accounts"

// Services is the console application surface assembled by api.New.
type Services struct {
	Accounts *Accounts
	Keys     *Keys
	Settings *Settings
	Backup   *Backup
}

func New(runtime Runtime) *Services {
	if runtime == nil {
		return nil
	}
	store := runtime.Store()
	return &Services{
		Accounts: NewAccounts(runtime),
		Keys:     NewKeys(store),
		Settings: NewSettings(store),
		Backup:   NewBackup(store),
	}
}

var _ Runtime = (*accounts.Manager)(nil)
