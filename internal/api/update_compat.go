package api

import (
	"time"

	appconsole "github.com/caigee-cmd/cli2api/internal/console"
	appupdate "github.com/caigee-cmd/cli2api/internal/update"
)

type systemUpdateJob = appupdate.Job

func parseQueryTime(raw string, endOfDay bool) *time.Time {
	return appconsole.ParseQueryTime(raw, endOfDay)
}
