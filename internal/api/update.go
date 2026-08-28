package api

import (
	"context"
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

type systemUpdateInfo struct {
	control.Info
	Agent control.AgentStatus `json:"agent"`
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
	writeJSON(w, http.StatusOK, systemUpdateInfo{Info: info, Agent: status})
}

func (s *Server) handleSystemUpdateApply(w http.ResponseWriter, r *http.Request) {
	if !s.updateRunning.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "update_in_progress", "An update is already in progress")
		return
	}
	accepted := false
	defer func() {
		if !accepted {
			s.maintenance.Store(false)
			s.updateRunning.Store(false)
		}
	}()

	info, err := s.updateChecker.Check(r.Context(), true)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "update_check_failed", err.Error())
		return
	}
	if !info.Managed {
		writeErr(w, http.StatusConflict, "update_not_managed", "Development builds cannot update from the console")
		return
	}
	if !info.HasUpdate || strings.TrimSpace(info.NextVersion) == "" {
		writeErr(w, http.StatusConflict, "already_up_to_date", "No next release is available")
		return
	}

	statusCtx, statusCancel := context.WithTimeout(context.Background(), 3*time.Second)
	status, err := s.updateAgent.Status(statusCtx)
	statusCancel()
	if err != nil || !status.Available {
		message := "Updater daemon is unavailable"
		if err != nil {
			message = err.Error()
		}
		writeErr(w, http.StatusServiceUnavailable, "updater_unavailable", message)
		return
	}
	if updaterStateActive(status.State) {
		writeErr(w, http.StatusConflict, "update_in_progress", "Updater daemon is busy")
		return
	}

	s.maintenance.Store(true)
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
	err = s.waitForUpdateIdle(drainCtx)
	drainCancel()
	if err != nil {
		writeErr(w, http.StatusConflict, "requests_in_flight", err.Error())
		return
	}

	backup, err := s.manager.Store().Backup(context.Background(), filepath.Join(s.cfg.DataDir, "backups"), 5)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "sqlite_backup_failed", err.Error())
		return
	}
	request := control.ApplyRequest{
		CurrentVersion: info.CurrentVersion,
		TargetVersion:  info.NextVersion,
		BackupPath:     filepath.Join("/data/backups", backup.Name),
	}
	applyCtx, applyCancel := context.WithTimeout(context.Background(), 8*time.Second)
	response, err := s.updateAgent.Apply(applyCtx, request)
	applyCancel()
	if err != nil {
		writeErr(w, http.StatusBadGateway, "update_submit_failed", err.Error())
		return
	}

	accepted = true
	go s.monitorUpdate(response.JobID)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"job_id": response.JobID, "current_version": info.CurrentVersion,
		"target_version": info.NextVersion, "backup": backup,
	})
}

func (s *Server) waitForUpdateIdle(ctx context.Context) error {
	for {
		_ = s.manager.RefreshAll(ctx, false)
		accounts, err := s.manager.Accounts(ctx)
		if err != nil {
			return err
		}
		inFlight := 0
		for _, account := range accounts {
			inFlight += account.InFlight
		}
		if inFlight == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%d request(s) are still in flight", inFlight)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (s *Server) monitorUpdate(jobID string) {
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		status, err := s.updateAgent.Status(ctx)
		cancel()
		if err == nil && (status.JobID == "" || status.JobID == jobID) {
			if status.State == "failed" {
				s.maintenance.Store(false)
				s.updateRunning.Store(false)
				return
			}
			if status.State == "rolled_back" || status.State == "succeeded" {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	s.maintenance.Store(false)
	s.updateRunning.Store(false)
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
