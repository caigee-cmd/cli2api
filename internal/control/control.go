package control

// Services is the console application surface assembled by app.New.
type Services struct {
	Accounts *Accounts
	Keys     *Keys
	Settings *Settings
	Backup   *Backup
	Catalog  *Catalog
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
