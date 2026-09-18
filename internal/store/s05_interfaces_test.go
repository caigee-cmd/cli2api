package store

import (
	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	"github.com/caigee-cmd/cli2api/internal/logs"
	"github.com/caigee-cmd/cli2api/internal/providers/devin"
	"github.com/caigee-cmd/cli2api/internal/providers/trae"
	"github.com/caigee-cmd/cli2api/internal/providers/workbuddy"
)

var _ accounts.AccountStore = (*Store)(nil)
var _ accounts.PoolStateStore = (*Store)(nil)
var _ logs.RequestStore = (*Store)(nil)
var _ logs.RequestPersister = (*Store)(nil)
var _ logs.RequestQuery = (*Store)(nil)
var _ auth.KeyLookup = (*Store)(nil)
var _ workbuddy.Store = (*Store)(nil)
var _ workbuddy.SecretReader = (*Store)(nil)
var _ workbuddy.ModelSettingReader = (*Store)(nil)
var _ trae.Store = (*Store)(nil)
var _ trae.SecretReader = (*Store)(nil)
var _ trae.ModelSettingReader = (*Store)(nil)
var _ trae.ModelMaxModeReader = (*Store)(nil)
var _ devin.Store = (*Store)(nil)
var _ devin.SecretReader = (*Store)(nil)
