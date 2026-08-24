package updater

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	control "github.com/caigee-cmd/cli2api/internal/update"
)

type ApplyRequest = control.ApplyRequest

type applier interface {
	Apply(context.Context, string, ApplyRequest, func(string)) (bool, error)
}

type Config struct {
	SocketPath    string
	ListenAddress string
	AuthToken     string
	StatusFile    string
}

type Service struct {
	config  Config
	applier applier
	mu      sync.Mutex
	status  control.AgentStatus
}

var backupNamePattern = regexp.MustCompile(`^qoder-[0-9]{8}T[0-9]{6}\.[0-9]{9}Z\.db$`)

func NewService(config Config, applier applier) *Service {
	service := &Service{config: config, applier: applier, status: control.AgentStatus{ProtocolVersion: control.AgentProtocolVersion, Available: true, State: "idle"}}
	service.loadStatus()
	return service
}

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/status", s.handleStatus)
	mux.HandleFunc("/v1/update", s.handleUpdate)
	if strings.TrimSpace(s.config.AuthToken) == "" {
		return mux
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("Authorization")
		expected := "Bearer " + s.config.AuthToken
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid updater token")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Service) Serve(ctx context.Context) error {
	listener, socketPath, err := s.listen()
	if err != nil {
		return err
	}
	defer listener.Close()
	if socketPath != "" {
		defer os.Remove(socketPath)
		if err := os.Chmod(socketPath, 0o660); err != nil {
			return err
		}
	}
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	err = server.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *Service) listen() (net.Listener, string, error) {
	if address := strings.TrimSpace(s.config.ListenAddress); address != "" {
		if strings.TrimSpace(s.config.AuthToken) == "" {
			return nil, "", fmt.Errorf("auth token required for TCP updater")
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return nil, "", fmt.Errorf("invalid listen address: %w", err)
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
			return nil, "", fmt.Errorf("TCP updater must listen on loopback")
		}
		listener, err := net.Listen("tcp", address)
		return listener, "", err
	}

	socketPath := strings.TrimSpace(s.config.SocketPath)
	if socketPath == "" {
		return nil, "", fmt.Errorf("socket path or listen address required")
	}
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o750); err != nil {
		return nil, "", err
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, "", fmt.Errorf("refusing to replace non-socket %s", socketPath)
		}
		if err := os.Remove(socketPath); err != nil {
			return nil, "", err
		}
	} else if !os.IsNotExist(err) {
		return nil, "", err
	}
	listener, err := net.Listen("unix", socketPath)
	return listener, socketPath, err
}

func (s *Service) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "protocol_version": control.AgentProtocolVersion})
}

func (s *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	s.mu.Lock()
	status := s.status
	s.mu.Unlock()
	status.ProtocolVersion = control.AgentProtocolVersion
	status.Available = true
	writeJSON(w, http.StatusOK, status)
}

func (s *Service) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var request ApplyRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateApplyRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	if isActiveState(s.status.State) {
		s.mu.Unlock()
		writeError(w, http.StatusConflict, "update already in progress")
		return
	}
	jobID, err := newJobID()
	if err != nil {
		s.mu.Unlock()
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	s.status = control.AgentStatus{
		ProtocolVersion: control.AgentProtocolVersion,
		Available:       true, State: "queued", JobID: jobID,
		CurrentVersion: request.CurrentVersion, TargetVersion: request.TargetVersion,
		BackupPath: request.BackupPath, StartedAt: now,
	}
	s.persistStatusLocked()
	s.mu.Unlock()

	writeJSON(w, http.StatusAccepted, control.ApplyResponse{JobID: jobID})
	go s.run(jobID, request)
}

func (s *Service) run(jobID string, request ApplyRequest) {
	time.Sleep(1500 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	rolledBack, err := s.applier.Apply(ctx, jobID, request, func(state string) {
		s.updateState(jobID, state, "", false)
	})
	if err == nil {
		s.updateState(jobID, "succeeded", "", true)
		return
	}
	state := "failed"
	if rolledBack {
		state = "rolled_back"
	}
	s.updateState(jobID, state, err.Error(), true)
}

func (s *Service) updateState(jobID, state, message string, finished bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status.JobID != jobID {
		return
	}
	s.status.State = state
	s.status.Error = message
	if finished {
		s.status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.persistStatusLocked()
}

func validateApplyRequest(request ApplyRequest) error {
	current, err := control.ParseVersion(request.CurrentVersion)
	if err != nil {
		return fmt.Errorf("invalid current version")
	}
	target, err := control.ParseVersion(request.TargetVersion)
	if err != nil || target.Compare(current) <= 0 {
		return fmt.Errorf("target version must be a newer stable release")
	}
	clean := path.Clean(request.BackupPath)
	if clean != request.BackupPath || path.Dir(clean) != "/data/backups" || !backupNamePattern.MatchString(path.Base(clean)) {
		return fmt.Errorf("invalid sqlite backup path")
	}
	return nil
}

func isActiveState(state string) bool {
	switch state {
	case "queued", "preparing", "pulling", "recreating", "checking", "rolling_back":
		return true
	default:
		return false
	}
}

func newJobID() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

func (s *Service) loadStatus() {
	if strings.TrimSpace(s.config.StatusFile) == "" {
		return
	}
	data, err := os.ReadFile(s.config.StatusFile)
	if err != nil {
		return
	}
	var status control.AgentStatus
	if json.Unmarshal(data, &status) != nil {
		return
	}
	status.ProtocolVersion = control.AgentProtocolVersion
	status.Available = true
	if isActiveState(status.State) {
		status.State = "failed"
		status.Error = "updater restarted during an active operation"
		status.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	s.status = status
}

func (s *Service) persistStatusLocked() {
	if strings.TrimSpace(s.config.StatusFile) == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.config.StatusFile), 0o700); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.status, "", "  ")
	if err != nil {
		return
	}
	_ = writeEnvFileAtomic(s.config.StatusFile, 0o600, append(data, '\n'))
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
