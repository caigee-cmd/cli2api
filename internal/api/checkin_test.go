package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
)

func TestUnifiedCheckinSettingsAndRoutes(t *testing.T) {
	server := New(config.Config{Host: "127.0.0.1", ProxyAPIKey: "test-key", QoderHome: t.TempDir(), DataDir: t.TempDir()})
	defer server.Close()
	request := func(method, path, body, key string) *httptest.ResponseRecorder {
		input := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if key != "" {
			input.Header.Set("Authorization", "Bearer "+key)
		}
		output := httptest.NewRecorder()
		server.Handler().ServeHTTP(output, input)
		return output
	}
	for _, body := range []string{`{"checkin_times":{"qoder":"10:30","workbuddy":"18:30"}}`, `{"workbuddy_checkin_time":"12:00"}`} {
		response := request(http.MethodPatch, "/api/system/settings", body, "test-key")
		if response.Code != http.StatusOK {
			t.Fatalf("settings: %d %s", response.Code, response.Body.String())
		}
	}
	response := request(http.MethodGet, "/api/system/settings", "", "test-key")
	if !bytes.Contains(response.Body.Bytes(), []byte(`"qoder":"10:30"`)) || !bytes.Contains(response.Body.Bytes(), []byte(`"workbuddy":"12:00"`)) {
		t.Fatal(response.Body.String())
	}
	for _, body := range []string{`{"checkin_times":{"devin":"10:00"}}`, `{"checkin_times":{"qoder":"25:00"}}`, `{"checkin_times":{"qoder":"9:00"}}`, `{"checkin_times":{"workbuddy":"10:00"},"workbuddy_checkin_time":"11:00"}`} {
		if response := request(http.MethodPatch, "/api/system/settings", body, "test-key"); response.Code != http.StatusBadRequest {
			t.Fatalf("invalid settings accepted: %s", body)
		}
	}
	response = request(http.MethodPost, "/api/accounts", `{"name":"CN","provider":"qoder","region":"cn","auto_checkin":true}`, "test-key")
	if response.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	var account accounts.Account
	if err := json.Unmarshal(response.Body.Bytes(), &account); err != nil {
		t.Fatal(err)
	}
	if !account.AutoCheckin || account.CheckinTime != "" {
		t.Fatalf("account=%+v", account)
	}
	path := "/api/accounts/" + account.ID
	if response := request(http.MethodPost, path+"/checkin", "{}", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unprotected checkin: %d", response.Code)
	}
	if response := request(http.MethodGet, path+"/checkins", "", ""); response.Code != http.StatusUnauthorized {
		t.Fatalf("unprotected history: %d", response.Code)
	}
	if response := request(http.MethodGet, path+"/checkins", "", "test-key"); response.Code != http.StatusOK {
		t.Fatalf("CN history gated incorrectly: %d %s", response.Code, response.Body.String())
	}
	for _, body := range []string{`{"checkin_time":"16:30"}`, `{"checkin_time":""}`} {
		if response := request(http.MethodPatch, path, body, "test-key"); response.Code != http.StatusOK {
			t.Fatalf("account settings: %d %s", response.Code, response.Body.String())
		}
	}
	stored, err := server.Manager.Store().Get(context.Background(), account.ID)
	if err != nil || stored.CheckinTime != "" {
		t.Fatalf("reset=%+v err=%v", stored, err)
	}
	response = request(http.MethodPost, "/api/accounts", `{"name":"Global","provider":"qoder","region":"global","auto_checkin":true}`, "test-key")
	if response.Code != http.StatusCreated {
		t.Fatalf("global create: %d %s", response.Code, response.Body.String())
	}
	var global accounts.Account
	if err := json.Unmarshal(response.Body.Bytes(), &global); err != nil {
		t.Fatal(err)
	}
	if !global.AutoCheckin {
		t.Fatalf("global account=%+v", global)
	}
	globalPath := "/api/accounts/" + global.ID
	if response := request(http.MethodGet, globalPath+"/checkins", "", "test-key"); response.Code != http.StatusOK {
		t.Fatalf("global history: %d %s", response.Code, response.Body.String())
	}
	response = request(http.MethodPost, globalPath+"/checkin", "{}", "test-key")
	if response.Code == http.StatusOK || bytes.Contains(response.Body.Bytes(), []byte("not available")) {
		t.Fatalf("global check-in was rejected as unsupported: %d %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("disabled")) {
		t.Fatalf("global check-in: %d %s", response.Code, response.Body.String())
	}
}
