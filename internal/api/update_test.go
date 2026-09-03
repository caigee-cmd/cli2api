package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/config"
	control "github.com/caigee-cmd/cli2api/internal/update"
)

type updateCheckerStub struct {
	info         control.Info
	err          error
	checkStarted chan struct{}
	checkRelease <-chan struct{}
	checkOnce    sync.Once
}

func (s *updateCheckerStub) Check(context.Context, bool) (control.Info, error) {
	if s.checkStarted != nil {
		s.checkOnce.Do(func() { close(s.checkStarted) })
		<-s.checkRelease
	}
	return s.info, s.err
}

type updateAgentStub struct {
	mu          sync.Mutex
	request     control.ApplyRequest
	status      control.AgentStatus
	err         error
	applyCalled chan struct{}
	applyOnce   sync.Once
}

func (s *updateAgentStub) Status(context.Context) (control.AgentStatus, error) {
	return s.status, s.err
}

func (s *updateAgentStub) Apply(_ context.Context, request control.ApplyRequest) (control.ApplyResponse, error) {
	s.mu.Lock()
	s.request = request
	s.mu.Unlock()
	if s.applyCalled != nil {
		s.applyOnce.Do(func() { close(s.applyCalled) })
	}
	if s.err != nil {
		return control.ApplyResponse{}, s.err
	}
	return control.ApplyResponse{JobID: "job-1"}, nil
}

func (s *updateAgentStub) requestSnapshot() control.ApplyRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.request
}

func waitForUpdateCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for update condition")
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
	agent := &updateAgentStub{
		status:      control.AgentStatus{Available: true, State: "failed", JobID: "job-1"},
		applyCalled: make(chan struct{}),
	}
	srv.updateChecker = checker
	srv.updateAgent = agent
	account, err := srv.manager.Store().Create(context.Background(), accounts.CreateAccount{Name: "busy", Enabled: false})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	srv.manager.Pool().Upsert(accounts.Item{ID: account.ID, Provider: "qoder", InFlight: 1})

	req := httptest.NewRequest(http.MethodPost, "/api/system/update", strings.NewReader(`{"target_version":"v9.9.9"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-agent.applyCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for asynchronous update submission")
	}
	request := agent.requestSnapshot()
	if request.CurrentVersion != "v0.2.1" || request.TargetVersion != "v0.2.2" {
		t.Fatalf("request = %+v", request)
	}
	if filepath.Dir(request.BackupPath) != "/data/backups" {
		t.Fatalf("backup path = %q", request.BackupPath)
	}
	backupPath := filepath.Join(dataDir, "backups", filepath.Base(request.BackupPath))
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
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	waitForUpdateCondition(t, func() bool {
		job := srv.snapshotUpdateJob()
		return job != nil && job.State == "failed"
	})
	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err == nil && len(entries) != 0 {
		t.Fatalf("unexpected backups: %v", entries)
	}
}

func TestSystemUpdateReturnsBeforePreparationCompletes(t *testing.T) {
	dataDir := t.TempDir()
	srv := New(config.Config{
		Host: "127.0.0.1", Port: 3010, ProxyAPIKey: "secret",
		QoderHome: t.TempDir(), DataDir: dataDir,
	})
	defer srv.Close()
	releaseCheck := make(chan struct{})
	checkStarted := make(chan struct{})
	checker := &updateCheckerStub{
		info:         control.Info{CurrentVersion: "v0.2.1", NextVersion: "v0.2.2", HasUpdate: true, Managed: true},
		checkStarted: checkStarted,
		checkRelease: releaseCheck,
	}
	agent := &updateAgentStub{
		status:      control.AgentStatus{Available: true, State: "failed", JobID: "job-1"},
		applyCalled: make(chan struct{}),
	}
	srv.updateChecker = checker
	srv.updateAgent = agent

	req := httptest.NewRequest(http.MethodPost, "/api/system/update", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	startedAt := time.Now()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("update submission took %s", elapsed)
	}
	select {
	case <-checkStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("asynchronous preparation did not start")
	}
	select {
	case <-agent.applyCalled:
		t.Fatal("update was submitted before preparation was released")
	default:
	}
	close(releaseCheck)
	select {
	case <-agent.applyCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for update submission")
	}
}
