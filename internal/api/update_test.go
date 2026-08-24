package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caigee-cmd/cli2api/internal/config"
	control "github.com/caigee-cmd/cli2api/internal/update"
)

type updateCheckerStub struct {
	info control.Info
	err  error
}

func (s *updateCheckerStub) Check(context.Context, bool) (control.Info, error) {
	return s.info, s.err
}

type updateAgentStub struct {
	request control.ApplyRequest
	status  control.AgentStatus
	err     error
}

func (s *updateAgentStub) Status(context.Context) (control.AgentStatus, error) {
	return s.status, s.err
}

func (s *updateAgentStub) Apply(_ context.Context, request control.ApplyRequest) (control.ApplyResponse, error) {
	s.request = request
	if s.err != nil {
		return control.ApplyResponse{}, s.err
	}
	return control.ApplyResponse{JobID: "job-1"}, nil
}

func TestSystemUpdateBacksUpSQLiteBeforeSubmittingNextVersion(t *testing.T) {
	dataDir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: dataDir,
	})
	defer srv.Close()
	checker := &updateCheckerStub{info: control.Info{
		CurrentVersion: "v0.2.1", NextVersion: "v0.2.2", HasUpdate: true, Managed: true,
	}}
	agent := &updateAgentStub{status: control.AgentStatus{Available: true, State: "failed", JobID: "job-1"}}
	srv.updateChecker = checker
	srv.updateAgent = agent

	req := httptest.NewRequest(http.MethodPost, "/api/system/update", strings.NewReader(`{"target_version":"v9.9.9"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if agent.request.CurrentVersion != "v0.2.1" || agent.request.TargetVersion != "v0.2.2" {
		t.Fatalf("request = %+v", agent.request)
	}
	if filepath.Dir(agent.request.BackupPath) != "/data/backups" {
		t.Fatalf("backup path = %q", agent.request.BackupPath)
	}
	backupPath := filepath.Join(dataDir, "backups", filepath.Base(agent.request.BackupPath))
	if _, err := os.Stat(backupPath); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestSystemUpdateDoesNotBackupWhenNoNextVersionExists(t *testing.T) {
	dataDir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: dataDir,
	})
	defer srv.Close()
	srv.updateChecker = &updateCheckerStub{info: control.Info{CurrentVersion: "v0.2.1", Managed: true}}
	srv.updateAgent = &updateAgentStub{}

	req := httptest.NewRequest(http.MethodPost, "/api/system/update", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("unexpected backups: %v", entries)
	}
}
