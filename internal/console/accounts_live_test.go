package console

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/control"
	"github.com/caigee-cmd/cli2api/internal/runtime"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

// A console page load must take the live path: the handler forces the
// synchronous upstream read instead of returning the stored projection.
func TestHandleAccountsRefreshUsesLiveRead(t *testing.T) {
	store, err := sqlstore.OpenStore(filepath.Join(t.TempDir(), "accounts-live.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	manager := runtime.NewManager(runtime.ManagerConfig{DataDir: t.TempDir()}, store, nil)
	t.Cleanup(func() { manager.Close() })
	h := &Handler{Control: control.New(manager)}

	for _, path := range []string{"/api/accounts?refresh=1", "/api/accounts"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.HandleAccounts(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var payload struct {
			Object string `json:"object"`
			Data   []any  `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s decode: %v", path, err)
		}
		if payload.Object != "list" {
			t.Fatalf("%s object=%q", path, payload.Object)
		}
	}
}
