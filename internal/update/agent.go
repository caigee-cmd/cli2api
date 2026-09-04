package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const AgentProtocolVersion = 2

type PrepareRequest struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
}

type ApplyRequest struct {
	CurrentVersion string `json:"current_version"`
	TargetVersion  string `json:"target_version"`
	BackupPath     string `json:"backup_path"`
	SkipPull       bool   `json:"skip_pull,omitempty"`
}

type ApplyResponse struct {
	JobID string `json:"job_id"`
}

type AgentStatus struct {
	ProtocolVersion int    `json:"protocol_version"`
	Available       bool   `json:"available"`
	StagedUpdate    bool   `json:"staged_update"`
	State           string `json:"state"`
	JobID           string `json:"job_id,omitempty"`
	CurrentVersion  string `json:"current_version,omitempty"`
	TargetVersion   string `json:"target_version,omitempty"`
	BackupPath      string `json:"backup_path,omitempty"`
	Error           string `json:"error,omitempty"`
	StartedAt       string `json:"started_at,omitempty"`
	FinishedAt      string `json:"finished_at,omitempty"`
}

type Agent interface {
	Status(context.Context) (AgentStatus, error)
	Apply(context.Context, ApplyRequest) (ApplyResponse, error)
}

type PreparedAgent interface {
	Prepare(context.Context, PrepareRequest) (ApplyResponse, error)
	ApplyPrepared(context.Context, ApplyRequest) (ApplyResponse, error)
}

type AgentClient struct {
	baseURL string
	token   string
	label   string
	client  *http.Client
}

func NewUnixAgentClient(socketPath string) *AgentClient {
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}
	return &AgentClient{
		baseURL: "http://unix",
		label:   "socket " + socketPath,
		client:  &http.Client{Transport: transport, Timeout: 8 * time.Second},
	}
}

func NewHTTPAgentClient(baseURL, token string) *AgentClient {
	client := &http.Client{Timeout: 8 * time.Second}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &AgentClient{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
		label:   strings.TrimSpace(baseURL),
		client:  client,
	}
}

func (c *AgentClient) Status(ctx context.Context) (AgentStatus, error) {
	var status AgentStatus
	if err := c.doJSON(ctx, http.MethodGet, "/v1/status", nil, &status); err != nil {
		return AgentStatus{Available: false, State: "unavailable", Error: err.Error()}, err
	}
	status.Available = true
	if status.ProtocolVersion != 0 && (status.ProtocolVersion < 1 || status.ProtocolVersion > AgentProtocolVersion) {
		return AgentStatus{Available: false, State: "unavailable", Error: fmt.Sprintf("unsupported updater protocol %d", status.ProtocolVersion)}, fmt.Errorf("unsupported updater protocol %d", status.ProtocolVersion)
	}
	return status, nil
}

func (c *AgentClient) Apply(ctx context.Context, request ApplyRequest) (ApplyResponse, error) {
	var response ApplyResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/update", request, &response); err != nil {
		return ApplyResponse{}, err
	}
	if strings.TrimSpace(response.JobID) == "" {
		return ApplyResponse{}, fmt.Errorf("updater returned empty job id")
	}
	return response, nil
}

func (c *AgentClient) ApplyPrepared(ctx context.Context, request ApplyRequest) (ApplyResponse, error) {
	var response ApplyResponse
	request.SkipPull = true
	if err := c.doJSON(ctx, http.MethodPost, "/v1/apply", request, &response); err != nil {
		return ApplyResponse{}, err
	}
	if strings.TrimSpace(response.JobID) == "" {
		return ApplyResponse{}, fmt.Errorf("updater returned empty job id")
	}
	return response, nil
}

func (c *AgentClient) Prepare(ctx context.Context, request PrepareRequest) (ApplyResponse, error) {
	var response ApplyResponse
	if err := c.doJSON(ctx, http.MethodPost, "/v1/prepare", request, &response); err != nil {
		return ApplyResponse{}, err
	}
	if strings.TrimSpace(response.JobID) == "" {
		return ApplyResponse{}, fmt.Errorf("updater returned empty job id")
	}
	return response, nil
}

func (c *AgentClient) doJSON(ctx context.Context, method, path string, input, output any) error {
	var bodyReader *strings.Reader
	if input == nil {
		bodyReader = strings.NewReader("")
	} else {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		bodyReader = strings.NewReader(string(data))
	}
	if c.baseURL == "" {
		return fmt.Errorf("updater endpoint required")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("connect updater %s: %w", c.label, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&failure)
		if failure.Error == "" {
			failure.Error = resp.Status
		}
		return fmt.Errorf("updater: %s", failure.Error)
	}
	if output != nil {
		if err := json.NewDecoder(resp.Body).Decode(output); err != nil {
			return fmt.Errorf("decode updater response: %w", err)
		}
	}
	return nil
}
