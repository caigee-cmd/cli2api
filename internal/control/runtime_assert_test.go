package control

import (
	accountruntime "github.com/caigee-cmd/cli2api/internal/runtime"
)

var _ Runtime = (*accountruntime.Manager)(nil)
