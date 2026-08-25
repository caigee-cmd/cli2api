package logs

import (
	"context"
	"log"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type RequestStore interface {
	InsertRequestLog(ctx context.Context, log accounts.RequestLog) error
	UpdateRequestLog(ctx context.Context, log accounts.RequestLog) error
	InsertRequestAttempt(ctx context.Context, attempt accounts.RequestAttempt) error
	PurgeRequestLogs(ctx context.Context, olderThan time.Duration, maxRows int) (int64, error)
	ClearRequestLogs(ctx context.Context) (int64, error)
	ListRequestLogs(ctx context.Context, filter accounts.RequestLogFilter) (accounts.RequestLogList, error)
	GetRequestLog(ctx context.Context, id string) (accounts.RequestLog, error)
}

type RequestRecorder struct {
	store RequestStore
	queue chan func()
}

func NewRequestRecorder(store RequestStore) *RequestRecorder {
	recorder := &RequestRecorder{
		store: store,
		queue: make(chan func(), 256),
	}
	go recorder.loop()
	return recorder
}

func (r *RequestRecorder) loop() {
	for fn := range r.queue {
		fn()
	}
}

func (r *RequestRecorder) enqueue(fn func()) {
	if r == nil {
		return
	}
	select {
	case r.queue <- fn:
	default:
		log.Printf("[logs] request recorder queue full, dropping write")
	}
}

func (r *RequestRecorder) Start(log accounts.RequestLog) {
	r.enqueue(func() {
		if err := r.store.InsertRequestLog(context.Background(), log); err != nil {
			logf("insert request log: %v", err)
		}
	})
}

func (r *RequestRecorder) Finish(log accounts.RequestLog) {
	r.enqueue(func() {
		if err := r.store.UpdateRequestLog(context.Background(), log); err != nil {
			logf("update request log: %v", err)
		}
	})
}

func (r *RequestRecorder) Attempt(attempt accounts.RequestAttempt) {
	r.enqueue(func() {
		if err := r.store.InsertRequestAttempt(context.Background(), attempt); err != nil {
			logf("insert request attempt: %v", err)
		}
	})
}

func (r *RequestRecorder) PurgeLoop(stop <-chan struct{}, every time.Duration) {
	if r == nil {
		return
	}
	if every <= 0 {
		every = time.Hour
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	r.purgeOnce()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			r.purgeOnce()
		}
	}
}

func (r *RequestRecorder) purgeOnce() {
	r.enqueue(func() {
		if _, err := r.store.PurgeRequestLogs(context.Background(), 7*24*time.Hour, 20_000); err != nil {
			logf("purge request logs: %v", err)
		}
	})
}

func (r *RequestRecorder) Store() RequestStore {
	if r == nil {
		return nil
	}
	return r.store
}

func logf(format string, args ...any) {
	log.Printf("[logs] "+format, args...)
}
