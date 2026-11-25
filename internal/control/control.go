package control

import "github.com/caigee-cmd/cli2api/internal/events"

// Services is the console application surface assembled by app.New.
type Services struct {
	Accounts     *Accounts
	Provider     *ProviderSettings
	Keys         *Keys
	Settings     *Settings
	Backup       *Backup
	Catalog      *Catalog
	Aggregations *Aggregations

	runtime Runtime
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
		runtime:      runtime,
	}
}

// SubscribeEvents exposes the real backend state-change stream (quota,
// check-in, health/cooldown, bulk progress) to the console SSE handler.
func (s *Services) SubscribeEvents(lastSeq uint64) (<-chan events.Event, func()) {
	if s == nil || s.runtime == nil {
		ch := make(chan events.Event)
		close(ch)
		return ch, func() {}
	}
	return s.runtime.SubscribeEvents(lastSeq)
}
