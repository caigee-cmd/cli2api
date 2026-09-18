package update

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type ReleaseChecker interface {
	Check(context.Context, bool) (Info, error)
}

type Coordinator struct {
	Checker ReleaseChecker
	Agent   Agent
	Backup  func(context.Context, string, int) (accounts.Backup, error)
	DataDir string

	Maintenance atomic.Bool
	Running     atomic.Bool
	mu          sync.Mutex
	Job         *Job
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    "api_error",
			"code":    code,
		},
	})
}

type preparedUpdateAgent interface {
	Prepare(context.Context, PrepareRequest) (ApplyResponse, error)
	ApplyPrepared(context.Context, ApplyRequest) (ApplyResponse, error)
}

type cancellableUpdateAgent interface {
	Cancel(context.Context) error
}

type Job struct {
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
	Info
	Agent  AgentStatus `json:"agent"`
	Update *Job        `json:"update,omitempty"`
}

func (c *Coordinator) Handle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		c.HandleInfo(w, r)
	case http.MethodPost:
		c.HandleApplyNow(w, r)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST only")
	}
}

func (c *Coordinator) HandlePrepare(w http.ResponseWriter, _ *http.Request) {
	if !c.Running.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "update_in_progress", "An update is already in progress")
		return
	}
	jobID, err := newSystemUpdateJobID()
	if err != nil {
		c.Running.Store(false)
		writeErr(w, http.StatusInternalServerError, "update_job_failed", err.Error())
		return
	}
	c.mu.Lock()
	c.Job = &Job{JobID: jobID, State: "preparing", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	c.mu.Unlock()
	go c.prepareImage(jobID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (c *Coordinator) HandleRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_request", "version is required")
		return
	}
	target := strings.TrimSpace(body.Version)
	if target == "" {
		writeErr(w, http.StatusBadRequest, "invalid_request", "version is required")
		return
	}
	if !c.Running.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "update_in_progress", "An update is already in progress")
		return
	}
	jobID, err := newSystemUpdateJobID()
	if err != nil {
		c.Running.Store(false)
		writeErr(w, http.StatusInternalServerError, "update_job_failed", err.Error())
		return
	}
	c.mu.Lock()
	c.Job = &Job{JobID: jobID, State: "preparing", TargetVersion: target, StartedAt: time.Now().UTC().Format(time.RFC3339)}
	c.mu.Unlock()
	c.Maintenance.Store(true)
	go c.rollback(jobID, target)
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "target_version": target})
}

func (c *Coordinator) HandleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
		return
	}
	statusCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	status, statusErr := c.Agent.Status(statusCtx)
	cancel()
	if statusErr != nil || !status.Available {
		message := "Updater daemon is unavailable"
		if statusErr != nil {
			message = statusErr.Error()
		}
		writeErr(w, http.StatusBadGateway, "updater_unavailable", message)
		return
	}
	job := c.Snapshot()
	if applyingPreparedUpdate(job) || updaterApplyActive(status) {
		writeErr(w, http.StatusConflict, "update_in_progress", "An update is already in progress")
		return
	}
	if !cancellablePreparedUpdate(job, status) {
		writeErr(w, http.StatusConflict, "update_not_cancellable", "No staged update is waiting to be cancelled")
		return
	}
	if hostCancellable(status) {
		agent, ok := c.Agent.(cancellableUpdateAgent)
		if !ok {
			writeErr(w, http.StatusConflict, "update_not_cancellable", "Host updater does not support cancelling staged updates")
			return
		}
		cancelCtx, cancelReq := context.WithTimeout(r.Context(), 8*time.Second)
		err := agent.Cancel(cancelCtx)
		cancelReq()
		if err != nil {
			writeErr(w, http.StatusBadGateway, "update_cancel_failed", err.Error())
			return
		}
	}
	if job != nil {
		c.Finish(job.JobID, "failed", "Update cancelled", true)
	}
	c.Running.Store(false)
	c.Maintenance.Store(false)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (c *Coordinator) HandleApply(w http.ResponseWriter, r *http.Request) {
	statusCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	status, _ := c.Agent.Status(statusCtx)
	cancel()
	job := c.adoptPrepared(status)
	if job == nil || job.State != "ready_to_apply" {
		writeErr(w, http.StatusConflict, "update_not_ready", "The target image is not ready to apply")
		return
	}
	jobID := job.JobID
	if !c.Running.Load() {
		writeErr(w, http.StatusConflict, "update_not_ready", "The update preparation has expired")
		return
	}
	c.Maintenance.Store(true)
	c.Mutate(jobID, func(job *Job) { job.State = "backing_up" })
	go c.applyPrepared(jobID)
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (c *Coordinator) HandleInfo(w http.ResponseWriter, r *http.Request) {
	info, err := c.Checker.Check(r.Context(), r.URL.Query().Get("force") == "1")
	if err != nil {
		writeErr(w, http.StatusBadGateway, "update_check_failed", err.Error())
		return
	}
	statusCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	status, statusErr := c.Agent.Status(statusCtx)
	if statusErr != nil {
		status = AgentStatus{Available: false, State: "unavailable", Error: statusErr.Error()}
	}
	job := c.adoptPrepared(status)
	if job == nil && status.State == "succeeded" && strings.TrimSpace(status.JobID) != "" {
		job = &Job{JobID: "agent-" + status.JobID, AgentJobID: status.JobID, State: "succeeded", CurrentVersion: status.CurrentVersion, TargetVersion: status.TargetVersion, StartedAt: status.StartedAt, FinishedAt: status.FinishedAt}
	}
	writeJSON(w, http.StatusOK, systemUpdateInfo{Info: info, Agent: status, Update: job})
}

func (c *Coordinator) HandleApplyNow(w http.ResponseWriter, _ *http.Request) {
	if !c.Running.CompareAndSwap(false, true) {
		writeErr(w, http.StatusConflict, "update_in_progress", "An update is already in progress")
		return
	}
	jobID, err := newSystemUpdateJobID()
	if err != nil {
		c.Running.Store(false)
		writeErr(w, http.StatusInternalServerError, "update_job_failed", err.Error())
		return
	}

	c.mu.Lock()
	c.Job = &Job{JobID: jobID, State: "preparing", StartedAt: time.Now().UTC().Format(time.RFC3339)}
	c.mu.Unlock()
	c.Maintenance.Store(true)
	go c.prepareOneShot(jobID)

	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

func (c *Coordinator) prepareImage(jobID string) {
	ctx := context.Background()
	setUpdateState := func(state string) {
		c.Mutate(jobID, func(job *Job) { job.State = state })
	}
	fail := func(message string) {
		c.Mutate(jobID, func(job *Job) {
			job.State = "failed"
			job.Error = message
			job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		})
		c.Running.Store(false)
	}

	setUpdateState("checking")
	info, err := c.Checker.Check(ctx, true)
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
	c.Mutate(jobID, func(job *Job) {
		job.CurrentVersion = info.CurrentVersion
		job.TargetVersion = info.NextVersion
	})

	statusCtx, statusCancel := context.WithTimeout(ctx, 3*time.Second)
	status, err := c.Agent.Status(statusCtx)
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
	if !status.StagedUpdate {
		c.Maintenance.Store(true)
		c.submitOneShot(jobID, info.CurrentVersion, info.NextVersion)
		return
	}
	agent, ok := c.Agent.(preparedUpdateAgent)
	if !ok {
		c.Maintenance.Store(true)
		c.submitOneShot(jobID, info.CurrentVersion, info.NextVersion)
		return
	}

	setUpdateState("submitting")
	request := PrepareRequest{CurrentVersion: info.CurrentVersion, TargetVersion: info.NextVersion}
	prepareCtx, prepareCancel := context.WithTimeout(ctx, 8*time.Second)
	response, err := agent.Prepare(prepareCtx, request)
	prepareCancel()
	if err != nil {
		fail(err.Error())
		return
	}
	c.Mutate(jobID, func(job *Job) {
		job.State = "preparing_image"
		job.AgentJobID = response.JobID
	})
	go c.monitorPrepared(jobID, response.JobID)
}

func (c *Coordinator) prepareOneShot(jobID string) {
	ctx := context.Background()
	setUpdateState := func(state string) {
		c.Mutate(jobID, func(job *Job) { job.State = state })
	}
	fail := func(message string) {
		c.Mutate(jobID, func(job *Job) {
			job.State = "failed"
			job.Error = message
			job.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		})
		c.Maintenance.Store(false)
		c.Running.Store(false)
	}

	setUpdateState("checking")
	info, err := c.Checker.Check(ctx, true)
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
	c.Mutate(jobID, func(job *Job) {
		job.CurrentVersion = info.CurrentVersion
		job.TargetVersion = info.NextVersion
	})

	statusCtx, statusCancel := context.WithTimeout(ctx, 3*time.Second)
	status, err := c.Agent.Status(statusCtx)
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

	c.submitOneShot(jobID, info.CurrentVersion, info.NextVersion)
}

func (c *Coordinator) rollback(jobID, targetVersion string) {
	ctx := context.Background()
	fail := func(message string) {
		c.Finish(jobID, "failed", message, true)
		c.Maintenance.Store(false)
		c.Running.Store(false)
	}
	c.Mutate(jobID, func(job *Job) { job.State = "checking" })
	info, err := c.Checker.Check(ctx, true)
	if err != nil {
		fail(err.Error())
		return
	}
	if !info.Managed {
		fail("Development builds cannot update from the console")
		return
	}
	allowed := false
	for _, release := range info.RollbackVersions {
		if release.TagName == targetVersion {
			allowed = true
			break
		}
	}
	if !allowed {
		fail("version is not in the allowed rollback list")
		return
	}
	c.Mutate(jobID, func(job *Job) {
		job.CurrentVersion = info.CurrentVersion
		job.TargetVersion = targetVersion
	})
	statusCtx, statusCancel := context.WithTimeout(ctx, 3*time.Second)
	status, err := c.Agent.Status(statusCtx)
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
	c.submitOneShot(jobID, info.CurrentVersion, targetVersion)
}

func (c *Coordinator) submitOneShot(jobID, currentVersion, targetVersion string) {
	ctx := context.Background()
	fail := func(message string) {
		c.Finish(jobID, "failed", message, true)
		c.Maintenance.Store(false)
		c.Running.Store(false)
	}
	c.Mutate(jobID, func(job *Job) { job.State = "backing_up" })
	if c.Backup == nil {
		fail("backup unavailable")
		return
	}
	backup, err := c.Backup(ctx, filepath.Join(c.DataDir, "backups"), 5)
	if err != nil {
		fail(err.Error())
		return
	}
	c.Mutate(jobID, func(job *Job) {
		job.BackupPath = filepath.Join("/data/backups", backup.Name)
		job.State = "submitting"
	})
	request := ApplyRequest{
		CurrentVersion: currentVersion,
		TargetVersion:  targetVersion,
		BackupPath:     filepath.Join("/data/backups", backup.Name),
	}
	applyCtx, applyCancel := context.WithTimeout(ctx, 8*time.Second)
	response, err := c.Agent.Apply(applyCtx, request)
	applyCancel()
	if err != nil {
		fail(err.Error())
		return
	}
	c.Mutate(jobID, func(job *Job) {
		job.State = "running"
		job.AgentJobID = response.JobID
	})
	go c.monitor(jobID, response.JobID)
}

func (c *Coordinator) applyPrepared(jobID string) {
	job := c.Snapshot()
	if job == nil || job.JobID != jobID {
		c.Finish(jobID, "failed", "Update preparation was lost", true)
		c.Maintenance.Store(false)
		c.Running.Store(false)
		return
	}
	agent, ok := c.Agent.(preparedUpdateAgent)
	if !ok {
		c.Finish(jobID, "failed", "Host updater does not support staged updates", true)
		c.Maintenance.Store(false)
		c.Running.Store(false)
		return
	}
	ctx := context.Background()
	if c.Backup == nil {
		c.Finish(jobID, "failed", "backup unavailable", true)
		c.Maintenance.Store(false)
		c.Running.Store(false)
		return
	}
	backup, err := c.Backup(ctx, filepath.Join(c.DataDir, "backups"), 5)
	if err != nil {
		c.Finish(jobID, "failed", err.Error(), true)
		c.Maintenance.Store(false)
		c.Running.Store(false)
		return
	}
	c.Mutate(jobID, func(job *Job) {
		job.BackupPath = filepath.Join("/data/backups", backup.Name)
		job.State = "submitting"
	})
	request := ApplyRequest{CurrentVersion: job.CurrentVersion, TargetVersion: job.TargetVersion, BackupPath: filepath.Join("/data/backups", backup.Name)}
	applyCtx, applyCancel := context.WithTimeout(ctx, 8*time.Second)
	response, err := agent.ApplyPrepared(applyCtx, request)
	applyCancel()
	if err != nil {
		c.Finish(jobID, "failed", err.Error(), true)
		c.Maintenance.Store(false)
		c.Running.Store(false)
		return
	}
	c.Mutate(jobID, func(job *Job) {
		job.State = "running"
		job.AgentJobID = response.JobID
	})
	go c.monitor(jobID, response.JobID)
}

func (c *Coordinator) monitorPrepared(jobID, agentJobID string) {
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		status, err := c.Agent.Status(ctx)
		cancel()
		if err == nil && (status.JobID == "" || status.JobID == agentJobID) {
			switch status.State {
			case "ready_to_apply":
				c.Mutate(jobID, func(job *Job) { job.State = "ready_to_apply" })
				return
			case "failed":
				c.Finish(jobID, "failed", status.Error, true)
				c.Running.Store(false)
				return
			case "idle":
				c.Finish(jobID, "failed", "Update cancelled", true)
				c.Running.Store(false)
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	c.Finish(jobID, "failed", "Image preparation timed out", true)
	c.Running.Store(false)
}

func (c *Coordinator) adoptPrepared(status AgentStatus) *Job {
	if status.State != "ready_to_apply" || strings.TrimSpace(status.JobID) == "" {
		return c.Snapshot()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Job != nil {
		if applyingPreparedUpdate(c.Job) {
			copy := *c.Job
			return &copy
		}
		c.Job.State = "ready_to_apply"
		c.Job.AgentJobID = status.JobID
		if c.Job.CurrentVersion == "" {
			c.Job.CurrentVersion = status.CurrentVersion
		}
		if c.Job.TargetVersion == "" {
			c.Job.TargetVersion = status.TargetVersion
		}
		c.Job.Error = ""
		c.Running.Store(true)
		copy := *c.Job
		return &copy
	}
	job := &Job{JobID: "agent-" + status.JobID, AgentJobID: status.JobID, State: "ready_to_apply", CurrentVersion: status.CurrentVersion, TargetVersion: status.TargetVersion, StartedAt: status.StartedAt}
	c.Job = job
	c.Running.Store(true)
	copy := *job
	return &copy
}

func applyingPreparedUpdate(job *Job) bool {
	if job == nil {
		return false
	}
	switch job.State {
	case "backing_up", "running":
		return true
	case "submitting":
		return job.BackupPath != ""
	default:
		return false
	}
}

func updaterApplyActive(status AgentStatus) bool {
	switch status.State {
	case "recreating", "rolling_back":
		return true
	case "queued", "preparing", "pulling", "host_binary", "checking":
		return status.BackupPath != ""
	default:
		return false
	}
}

func cancellablePreparedUpdate(job *Job, status AgentStatus) bool {
	if applyingPreparedUpdate(job) || updaterApplyActive(status) {
		return false
	}
	if status.State == "ready_to_apply" {
		return true
	}
	if job != nil {
		switch job.State {
		case "preparing", "checking", "submitting", "preparing_image":
			return job.BackupPath == ""
		}
	}
	switch status.State {
	case "queued", "pulling", "host_binary", "preparing", "image_ready":
		return status.BackupPath == ""
	default:
		return false
	}
}

func hostCancellable(status AgentStatus) bool {
	if status.State == "ready_to_apply" {
		return true
	}
	switch status.State {
	case "queued", "pulling", "host_binary", "preparing", "image_ready":
		return status.BackupPath == ""
	default:
		return false
	}
}

func (c *Coordinator) ReplaceJob(job *Job) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Job = job
}

func (c *Coordinator) Snapshot() *Job {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Job == nil {
		return nil
	}
	copy := *c.Job
	return &copy
}

func (c *Coordinator) Mutate(jobID string, mutate func(*Job)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Job == nil || c.Job.JobID != jobID {
		return false
	}
	mutate(c.Job)
	return true
}

func (c *Coordinator) monitor(jobID, agentJobID string) {
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		status, err := c.Agent.Status(ctx)
		cancel()
		if err == nil && (status.JobID == "" || status.JobID == agentJobID) {
			if status.State == "failed" {
				c.Finish(jobID, "failed", status.Error, true)
				c.Maintenance.Store(false)
				c.Running.Store(false)
				return
			}
			if status.State == "rolled_back" {
				c.Finish(jobID, "rolled_back", status.Error, true)
				c.Maintenance.Store(false)
				c.Running.Store(false)
				return
			}
			if status.State == "succeeded" {
				c.Finish(jobID, "succeeded", "", true)
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	c.Finish(jobID, "failed", "Updater health check timed out", true)
	c.Maintenance.Store(false)
	c.Running.Store(false)
}

func (c *Coordinator) Finish(jobID, state, message string, finished bool) {
	c.Mutate(jobID, func(job *Job) {
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
	case "queued", "preparing", "pulling", "host_binary", "recreating", "checking", "rolling_back", "ready_to_apply":
		return true
	default:
		return false
	}
}

func BlocksDuringUpdate(path string) bool {
	if path == "/api/system/update" || path == "/api/system/update/prepare" || path == "/api/system/update/apply" || path == "/api/system/update/cancel" || path == "/api/system/update/rollback" || path == "/health" {
		return false
	}
	return strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/v1/")
}
