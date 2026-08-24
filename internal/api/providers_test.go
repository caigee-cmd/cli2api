package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/config"
)

func newProviderTestServer(t *testing.T) *Server {
	t.Helper()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: t.TempDir(), RuntimeDir: t.TempDir(),
		WorkerDaemonPath: "/dev/null",
	})
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

func TestAccountsRejectUnknownProvider(t *testing.T) {
	srv := newProviderTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", bytes.NewBufferString(
		`{"name":"X","provider":"cursor","enabled":false}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown provider status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAccountsCreatePersistsProviderAndRegion(t *testing.T) {
	srv := newProviderTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/accounts", bytes.NewBufferString(
		`{"name":"WB","provider":"workbuddy","region":"cn","enabled":false}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID       string `json:"id"`
		Provider string `json:"provider"`
		Region   string `json:"region"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Provider != "workbuddy" || created.Region != "cn" {
		t.Fatalf("created = %+v", created)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	list.Header.Set("Authorization", "Bearer secret")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, list)
	var listed struct {
		Data []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			Region   string `json:"region"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Provider != "workbuddy" || listed.Data[0].Region != "cn" {
		t.Fatalf("listed = %+v", listed.Data)
	}
	for _, body := range []string{rec.Body.String(), listRec.Body.String()} {
		if bytes.Contains(bytes.ToLower([]byte(body)), []byte("access_token")) ||
			bytes.Contains(bytes.ToLower([]byte(body)), []byte("refresh_token")) {
			t.Fatalf("account response leaked credential fields: %s", body)
		}
	}
}

func TestProvidersEndpointExposesDescriptors(t *testing.T) {
	srv := newProviderTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/providers", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("providers status = %d", rec.Code)
	}
	var parsed struct {
		Data []struct {
			ID      string `json:"id"`
			Runtime string `json:"runtime"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Data) < 2 {
		t.Fatalf("expected qoder + workbuddy descriptors, got %+v", parsed.Data)
	}
}
