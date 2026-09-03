package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	control "github.com/caigee-cmd/cli2api/internal/update"
)

type updateChecker interface {
	Check(context.Context, bool) (control.Info, error)
}

type updateAgent interface {
	Status(context.Context) (control.AgentStatus, error)
	Apply(context.Context, control.ApplyRequest) (control.ApplyResponse, error)
}

type systemUpdateJob struct {
	JobID          string `json:"job_id"`
	AgentJobID     string `json:"agent_job_id,omitempty"`
	State          string `json:"state"`
	CurrentVersion string `json:"current_version,omitempty"`
	TargetVersion  string `json:"target_version,omitempty"`
	BackupPath     string `json:"backup_path,omitempty"`
	Error          string `json:"error,omitempty"`
	StartedAt      string `json:"started_at,omitempty"`
	FinishedAt     string `json:"finished_at,omitempty"`
}

type systemUpdateInfo struct {
	control.Info
	Agent  control.AgentStatus `json:"agent"`
	Update *systemUpdateJob    `json:"update,omitempty"`
}

func (s *Server) handleSystemUpdate(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleSystemUpdateInfo(w, r)
	case http.MethodPost:
		s.handleSystemUpdateApply(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
}

func (s *Server) handleSystemUpdateInfo(w http.ResponseWriter, r *http.Request) {
	info, err := s.updateChecker.Check(r.Context(), r.URL.Query().Get("force") == "1")
	if err != nil {
		writeErr(w, http.StatusBadGateway, "update_check_failed", err.Error())
		return
	}
	statusCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	status, statusErr := s.updateAgent.Status(statusCtx)
	if statusErr != nil {
		status = control.AgentStatus{Available: false, State: "unavailable", Error: statusErr.Error()}
	}
	writeJSON(w, http.StatusOK, systemUpdateInfo{Info: info, Agent: status, Update: s.snapshotUpdateJob()})
}

func (s *Server) handleSystemUpdateApply(w http.ResponseWriter, _ *http.Request) {
	if !s.updateRunning.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "update_in_progress", "An update is already in progress")
		return
	}
	jobID, err := newSystemUpdateJobID()
	if err != nil {
		s.updateRunning.Store(false)
		writeErr(w, http.StatusInternalServerError, "update_job_failed", err.Error())
		return
	}

	s.updateMu.Lock()
	s.updateJob = &systemUpdateJob{JobID: jobID, State: "preparing", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	s.updateMu.Unlock()
	s.maintenance.Store(true)
	go s.prepareSystemUpdate(jobID)

	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (s *Server) prepareSystemUpdate(jobID string) {
	ctx := context.Background()
	setUpdateState := func(state string) {
		s.mutateUpdateJob(jobID, func(job *systemUpdateJob) { job.State = state })
	}
	fail := func(message string) {
		s.mutateUpdateJob(jobID, func(job *systemUpdateJob) {
			job.State = "failed"
			job.Error = message
			job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		})
		s.maintenance.Store(false)
		s.updateRunning.Store(false)
	}

	setUpdateState("checking")
	info, err := s.updateChecker.Check(ctx, true)
	if err != nil {
		fail(err.Error())
		return
	}
	if !info.Managed {
		fail("Development builds cannot update from the console")
		return
	}
	if !info.HasUpdate || strings.TrimSpace(info.NextVersion) == "" {
		fail("No next release is available")
		return
	}
	s.mutateUpdateJob(jobID, func(job *systemUpdateJob) {
		job.CurrentVersion = info.CurrentVersion
		job.TargetVersion = info.NextVersion
	})

	statusCtx, statusCancel := context.WithTimeout(ctx, 3*time.Second)
	status, err := s.updateAgent.Status(statusCtx)
	statusCancel()
	if err != nil || !status.Available {
		message := "Updater daemon is unavailable"
		if err != nil {
			message = err.Error()
		}
		fail(message)
		return
	}
	if updaterStateActive(status.State) {
		fail("Updater daemon is busy")
		return
	}

	setUpdateState("backing_up")
	backup, err := s.manager.Store().Backup(ctx, filepath.Join(s.cfg.DataDir, "backups"), 5)
	if err != nil {
		fail(err.Error())
		return
	}
	s.mutateUpdateJob(jobID, func(job *systemUpdateJob) {
		job.BackupPath = filepath.Join("/data/backups", backup.Name)
	})

	setUpdateState("submitting")
	request := control.ApplyRequest{
		CurrentVersion: info.CurrentVersion,
		TargetVersion:  info.NextVersion,
		BackupPath:     filepath.Join("/data/backups", backup.Name),
	}
	applyCtx, applyCancel := context.WithTimeout(ctx, 8*time.Second)
	response, err := s.updateAgent.Apply(applyCtx, request)
	applyCancel()
	if err != nil {
		fail(err.Error())
		return
	}

	s.mutateUpdateJob(jobID, func(job *systemUpdateJob) {
		job.State = "running"
		job.AgentJobID = response.JobID
	})
	go s.monitorUpdate(jobID, response.JobID)
}

func (s *Server) snapshotUpdateJob() *systemUpdateJob {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateJob == nil {
		return nil
	}
	copy := *s.updateJob
	return &copy
}

func (s *Server) mutateUpdateJob(jobID string, mutate func(*systemUpdateJob)) bool {
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if s.updateJob == nil || s.updateJob.JobID != jobID {
		return false
	}
	mutate(s.updateJob)
	return true
}

func (s *Server) monitorUpdate(jobID, agentJobID string) {
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		status, err := s.updateAgent.Status(ctx)
		cancel()
		if err == nil && (status.JobID == "" || status.JobID == agentJobID) {
			if status.State == "failed" {
				s.finishUpdateJob(jobID, "failed", status.Error, true)
				s.maintenance.Store(false)
				s.updateRunning.Store(false)
				return
			}
			if status.State == "rolled_back" {
				s.finishUpdateJob(jobID, "rolled_back", status.Error, true)
				s.maintenance.Store(false)
				s.updateRunning.Store(false)
				return
			}
			if status.State == "succeeded" {
				s.finishUpdateJob(jobID, "succeeded", "", true)
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	s.finishUpdateJob(jobID, "failed", "Updater health check timed out", true)
	s.maintenance.Store(false)
	s.updateRunning.Store(false)
}

func (s *Server) finishUpdateJob(jobID, state, message string, finished bool) {
	s.mutateUpdateJob(jobID, func(job *systemUpdateJob) {
		job.State = state
		job.Error = message
		if finished {
			job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		}
	})
}

func newSystemUpdateJobID() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate update job id: %w", err)
	}
	return "update-" + hex.EncodeToString(value), nil
}

func updaterStateActive(state string) bool {
	switch state {
	case "queued", "preparing", "pulling", "recreating", "checking", "rolling_back":
		return true
	default:
		return false
	}
}

func blocksDuringUpdate(path string) bool {
	if path == "/api/system/update" || path == "/health" {
		return false
	}
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/")
}
