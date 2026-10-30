package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/auth"
	applogs "github.com/caigee-cmd/cli2api/internal/logs"
	sqlstore "github.com/caigee-cmd/cli2api/internal/store"
)

// keyedFixture creates one named API key, sends a request through it, then
// sends a console-key request. Everything is console-key traffic otherwise.
func keyedFixture(t *testing.T, server *Server, store *sqlstore.Store) accounts.APIKey {
	t.Helper()
	secret := "sk_fixture_ci_secret"
	stored, err := store.InsertAPIKey(context.Background(), accounts.StoredAPIKey{
		Name: "ci", Prefix: accounts.APIKeyPrefix(secret), KeyHash: accounts.HashAPIKey(secret),
		Providers: []string{"qoder"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored.Secret = secret
	server.Recorder = applogs.NewRequestRecorder(store)
	server.Auth = auth.NewVerifier("secret", store)
	server.RebuildHTTP()

	send := func(secret, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := send(stored.Secret, `{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hi"}]}`); rec.Code != http.StatusOK {
		t.Fatalf("keyed call status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := send("secret", `{"model":"qoder/glm-5.2","messages":[{"role":"user","content":"hi"}]}`); rec.Code != http.StatusOK {
		t.Fatalf("console call status=%d body=%s", rec.Code, rec.Body.String())
	}
	return stored
}

// waitForAttributedLogs blocks until both fixture requests have landed so the
// filters below are not racing the asynchronous recorder.
func waitForAttributedLogs(t *testing.T, store *sqlstore.Store, keyID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	var last accounts.RequestLogList
	for time.Now().Before(deadline) {
		list, err := store.ListRequestLogs(context.Background(), accounts.RequestLogFilter{APIKeyID: keyID, Limit: 10})
		if err == nil && list.Total == 1 && list.Items[0].APIKeyID == keyID {
			return
		}
		if err == nil {
			last = list
		}
		time.Sleep(20 * time.Millisecond)
	}
	all, _ := store.ListRequestLogs(context.Background(), accounts.RequestLogFilter{Limit: 10})
	t.Fatalf("keyed request never got attributed to the API key: filtered=%+v all=%+v", last, all)
}

func TestAPIKeyAttributionAndFilters(t *testing.T) {
	server, closeServer := newCompatibilityServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(openaiNonStreamBody()))
	})
	defer closeServer()
	store, err := sqlstore.OpenStore(t.TempDir() + "/qoder.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	server.Recorder = applogs.NewRequestRecorder(store)
	server.Auth = auth.NewVerifier("secret", store)
	server.RebuildHTTP()

	key := keyedFixture(t, server, store)
	waitForAttributedLogs(t, store, key.ID)

	ctx := context.Background()
	// Only the keyed request names a key; the console call leaves it empty.
	all, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if all.Total != 2 {
		t.Fatalf("total = %d, want 2", all.Total)
	}
	// An explicit window: hours=24 truncates `to` down to 10s, which can exclude
	// rows written moments ago.
	from := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	to := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	window := "from=" + from + "&to=" + to
	byKey, err := store.ListRequestLogs(ctx, accounts.RequestLogFilter{APIKeyID: key.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if byKey.Total != 1 || byKey.Items[0].APIKeyID != key.ID {
		t.Fatalf("api_key filter = %+v", byKey)
	}

	// Stats and usage endpoints honour ?api_key= and keep the unfiltered
	// window intact.
	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		return rec
	}

	var keyed accounts.RequestStats
	if err := json.Unmarshal(get("/api/logs/stats?"+window+"&api_key="+key.ID).Body.Bytes(), &keyed); err != nil {
		t.Fatal(err)
	}
	if keyed.Totals.Requests != 1 {
		t.Fatalf("keyed stats requests = %d, want 1", keyed.Totals.Requests)
	}
	var every accounts.RequestStats
	if err := json.Unmarshal(get("/api/logs/stats?"+window).Body.Bytes(), &every); err != nil {
		t.Fatal(err)
	}
	if every.Totals.Requests != 2 {
		t.Fatalf("unfiltered stats requests = %d, want 2", every.Totals.Requests)
	}

	var keyedUsage accounts.RequestUsage
	if err := json.Unmarshal(get("/api/logs/usage?"+window+"&api_key="+key.ID).Body.Bytes(), &keyedUsage); err != nil {
		t.Fatal(err)
	}
	if keyedUsage.Totals.Requests != 1 {
		t.Fatalf("keyed usage requests = %d, want 1", keyedUsage.Totals.Requests)
	}

	var keyedCost accounts.CostBreakdown
	if err := json.Unmarshal(get("/api/usage/cost?"+window+"&api_key="+key.ID).Body.Bytes(), &keyedCost); err != nil {
		t.Fatal(err)
	}
	if keyedCost.Totals.Requests != 1 {
		t.Fatalf("keyed cost requests = %d, want 1", keyedCost.Totals.Requests)
	}

	var listed accounts.RequestLogList
	if err := json.Unmarshal(get("/api/logs/requests?api_key="+key.ID).Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 1 {
		t.Fatalf("logs api_key filter total = %d, want 1", listed.Total)
	}
	if listed.Items[0].APIKeyID != key.ID {
		t.Fatalf("listed api_key_id = %q", listed.Items[0].APIKeyID)
	}
}
