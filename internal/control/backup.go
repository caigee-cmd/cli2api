package control

import (
	"context"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type Backup struct {
	store accounts.AccountStore
}

func NewBackup(store accounts.AccountStore) *Backup {
	if store == nil {
		return nil
	}
	return &Backup{store: store}
}

func (b *Backup) Snapshot(ctx context.Context, directory string, keep int) (accounts.Backup, error) {
	return b.store.Backup(ctx, directory, keep)
}
