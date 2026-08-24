package update

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestUnixAgentClientSubmitsFixedUpdateRequest(t *testing.T) {
	socketPath := filepath.Join(os.TempDir(), "cli2api-updater-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
	t.Cleanup(func() { _ = os.Remove(socketPath) })
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/update" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var request ApplyRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.CurrentVersion != "v0.2.1" || request.TargetVersion != "v0.2.2" || request.BackupPath != "/data/backups/qoder-test.db" {
			t.Fatalf("request = %+v", request)
		}
		_ = json.NewEncoder(w).Encode(ApplyResponse{JobID: "job-1"})
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	client := NewUnixAgentClient(socketPath)
	response, err := client.Apply(context.Background(), ApplyRequest{
		CurrentVersion: "v0.2.1",
		TargetVersion:  "v0.2.2",
		BackupPath:     "/data/backups/qoder-test.db",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.JobID != "job-1" {
		t.Fatalf("response = %+v", response)
	}
}

func TestHTTPAgentClientUsesBearerToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer local-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(AgentStatus{ProtocolVersion: AgentProtocolVersion, Available: true, State: "idle"})
	}))
	defer server.Close()

	client := NewHTTPAgentClient(server.URL, "local-token")
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.State != "idle" {
		t.Fatalf("status = %+v", status)
	}
}

func TestHTTPAgentClientRejectsUnknownProtocol(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(AgentStatus{ProtocolVersion: AgentProtocolVersion + 1, Available: true, State: "idle"})
	}))
	defer server.Close()

	client := NewHTTPAgentClient(server.URL, "")
	status, err := client.Status(context.Background())
	if err == nil || status.Available {
		t.Fatalf("status = %+v err = %v", status, err)
	}
}
