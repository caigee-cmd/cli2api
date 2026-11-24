package control

// Services is the console application surface assembled by app.New.
type Services struct {
	Accounts     *Accounts
	Provider     *ProviderSettings
	Keys         *Keys
	Settings     *Settings
	Backup       *Backup
	Catalog      *Catalog
	Aggregations *Aggregations
}

func New(runtime Runtime) *Services {
	if runtime == nil {
		return nil
	}
	store := runtime.Store()
	provider := NewProviderSettings(runtime)
	return &Services{
		Accounts:     NewAccounts(runtime),
		Provider:     provider,
		Keys:         NewKeys(store),
		Settings:     NewSettings(store),
		Backup:       NewBackup(store),
		Aggregations: NewAggregations(store, provider),
	}
}
