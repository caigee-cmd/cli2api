package logs

import (
	"context"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type persistOnly struct{}

func (persistOnly) InsertRequestLog(context.Context, accounts.RequestLog) error {
	return nil
}
func (persistOnly) UpdateRequestLog(context.Context, accounts.RequestLog) error {
	return nil
}
func (persistOnly) InsertRequestAttempt(context.Context, accounts.RequestAttempt) error {
	return nil
}
func (persistOnly) InsertRequestStreamDiagnostic(context.Context, accounts.RequestStreamDiagnostic) error {
	return nil
}
func (persistOnly) InsertRequestUsageDetail(context.Context, accounts.RequestUsageDetail) error {
	return nil
}
func (persistOnly) PurgeRequestLogs(context.Context, time.Duration, int) (int64, error) {
	return 0, nil
}

func TestRequestRecorderStoreRequiresQuerySurface(t *testing.T) {
	recorder := NewRequestRecorder(persistOnly{})
	if recorder.Store() != nil {
		t.Fatal("persist-only recorder should not advertise RequestQuery")
	}
}
