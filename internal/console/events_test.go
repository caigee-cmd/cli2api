package console

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/events"
	"github.com/caigee-cmd/cli2api/internal/runtime"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

// The SSE endpoint must stream the real backend event envelope (seq/type/time
// and data) with SSE framing, and honor the documented authentication-wrapped
// route shape used by the server.
func TestHandleEventsStreamsRealFrames(t *testing.T) {
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "sse.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := runtime.NewManager(runtime.ManagerConfig{DataDir: t.TempDir()}, store, nil)
	t.Cleanup(func() { manager.Close() })
	h := &Handler{Control: control.New(manager)}

	server := httptest.NewServer(http.HandlerFunc(h.HandleEvents))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type=%q", got)
	}

	// Trigger a real structural change after the stream is open.
	account, err := store.Create(context.Background(), accounts.CreateAccount{
		Name: "sse-account", Provider: "qoder", Region: "cn", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.SyncAccount(context.Background(), account, account); err != nil {
		t.Fatal(err)
	}

	type result struct {
		event events.Event
		err   error
	}
	found := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var event events.Event
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
				found <- result{err: err}
				return
			}
			if event.Type == events.TypeAccounts {
				found <- result{event: event}
				return
			}
		}
		found <- result{err: context.DeadlineExceeded}
	}()

	select {
	case got := <-found:
		if got.err != nil {
			t.Fatalf("stream read: %v", got.err)
		}
		if got.event.Seq == 0 || got.event.Time == "" || got.event.AccountID != account.ID {
			t.Fatalf("event = %+v", got.event)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no real event frame arrived")
	}
}
