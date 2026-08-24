package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

type ManagerConfig struct {
	DataDir       string
	BasePort      int
	NodeBinary    string
	DaemonPath    string
	QoderCLIPath  string
	TemplatePath  string
	ProxyAPIKey   string
	MaxLogWriters io.Writer
	RestartDelay  time.Duration
}

type ManagedProcess interface {
	URL() string
	Done() <-chan error
	Stop() error
}

type ProcessStarter interface {
	Start(context.Context, Account, string, int) (ManagedProcess, error)
}

type Manager struct {
	config     ManagerConfig
	store      *Store
	starter    ProcessStarter
	pool       *Pool
	mu         sync.Mutex
	processes  map[string]ManagedProcess
	restarts   map[string]int
	nextPort   int
	httpClient *http.Client
	runCtx     context.Context
	cancel     context.CancelFunc
}

func NewManager(config ManagerConfig, store *Store, starter ProcessStarter) *Manager {
	if config.BasePort <= 0 {
		config.BasePort = 32100
	}
	if config.RestartDelay <= 0 {
		config.RestartDelay = time.Second
	}
	if starter == nil {
		starter = ExecStarter{Config: config}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	manager := &Manager{
		config:     config,
		store:      store,
		starter:    starter,
		pool:       NewPool(nil, nil),
		processes:  map[string]ManagedProcess{},
		restarts:   map[string]int{},
		nextPort:   config.BasePort,
		httpClient: &http.Client{Timeout: 2 * time.Second},
		runCtx:     runCtx,
		cancel:     cancel,
	}
	manager.pool.SetObserver(func(item Item) {
		_ = manager.store.RecordPoolState(context.Background(), item)
	})
	return manager
}

func (m *Manager) Start(ctx context.Context) error {
	accounts, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if err := m.startAccount(ctx, account); err != nil {
			m.pool.MarkDown(account.ID, 0, err.Error())
		}
	}
	return nil
}

func (m *Manager) Pool() *Pool   { return m.pool }
func (m *Manager) Store() *Store { return m.store }

func (m *Manager) Close() error {
	m.cancel()
	m.mu.Lock()
	processes := make([]ManagedProcess, 0, len(m.processes))
	for _, process := range m.processes {
		processes = append(processes, process)
	}
	m.processes = map[string]ManagedProcess{}
	m.mu.Unlock()
	var joined error
	for _, process := range processes {
		joined = errors.Join(joined, process.Stop())
	}
	return joined
}

func (m *Manager) startAccount(ctx context.Context, account Account) error {
	descriptor, _, err := providers.Resolve(account.Provider, account.ProviderRegion)
	if err != nil {
		return err
	}
	if descriptor.Runtime == providers.RuntimeInProcess {
		m.pool.Upsert(Item{
			ID: account.ID, Provider: descriptor.ID, Region: account.ProviderRegion,
			Runtime: string(descriptor.Runtime),
		})
		return nil
	}
	m.mu.Lock()
	if _, exists := m.processes[account.ID]; exists {
		m.mu.Unlock()
		return nil
	}
	port := m.nextPort
	m.nextPort++
	m.mu.Unlock()

	home := filepath.Join(m.config.DataDir, "runtime", account.ID)
	if err := materializeHome(ctx, m.store, account.ID, home); err != nil {
		return err
	}
	process, err := m.starter.Start(ctx, account, home, port)
	if err != nil {
		return fmt.Errorf("start account %s: %w", account.ID, err)
	}
	m.mu.Lock()
	m.processes[account.ID] = process
	m.mu.Unlock()
	m.mu.Lock()
	restarts := m.restarts[account.ID]
	m.mu.Unlock()
	m.pool.Upsert(Item{
		ID: account.ID, URL: process.URL(), Provider: descriptor.ID,
		Region: account.ProviderRegion, Runtime: string(descriptor.Runtime), Restarts: restarts,
	})
	go m.watchAccount(account.ID, process)
	return nil
}

func materializeHome(ctx context.Context, store *Store, accountID, home string) error {
	authDir := filepath.Join(home, ".qoder", ".auth")
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		return fmt.Errorf("create account home: %w", err)
	}
	credential, err := store.LoadCredential(ctx, accountID)
	if errors.Is(err, ErrAccountNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(authDir, "user"), credential.UserBlob, 0o600); err != nil {
		return fmt.Errorf("write user credential: %w", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "machine_id"), []byte(credential.MachineID), 0o600); err != nil {
		return fmt.Errorf("write machine id: %w", err)
	}
	return nil
}

type ExecStarter struct {
	Config ManagerConfig
}

type execProcess struct {
	cmd  *exec.Cmd
	url  string
	done chan error
}

func (p *execProcess) URL() string        { return p.url }
func (p *execProcess) Done() <-chan error { return p.done }
func (p *execProcess) Stop() error {
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	if err := p.cmd.Process.Signal(os.Interrupt); err == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (s ExecStarter) Start(_ context.Context, account Account, home string, port int) (ManagedProcess, error) {
	node := s.Config.NodeBinary
	if node == "" {
		node = "node"
	}
	if s.Config.DaemonPath == "" {
		return nil, fmt.Errorf("worker daemon path required")
	}
	cmd := exec.Command(node, s.Config.DaemonPath)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"QODER_HOME="+filepath.Join(home, ".qoder"),
		"QODER_ACCOUNT_ID="+account.ID,
		"QODER_MAX_INFLIGHT="+strconv.Itoa(account.MaxInFlight),
		"WORKER_HOST=127.0.0.1",
		"WORKER_PORT="+strconv.Itoa(port),
		"PROXY_API_KEY="+s.Config.ProxyAPIKey,
		"QODERCLI_JS="+s.Config.QoderCLIPath,
		"PLAIN_TEMPLATE_PATH="+s.Config.TemplatePath,
		"QODER_WARMUP_CWD="+filepath.Join(home, "work"),
	)
	writer := s.Config.MaxLogWriters
	if writer == nil {
		writer = os.Stderr
	}
	cmd.Stdout = writer
	cmd.Stderr = writer
	if err := os.MkdirAll(filepath.Join(home, "work"), 0o700); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	process := &execProcess{cmd: cmd, url: "http://127.0.0.1:" + strconv.Itoa(port), done: make(chan error, 1)}
	go func() {
		process.done <- cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

func (m *Manager) Create(ctx context.Context, input CreateAccount) (Account, error) {
	account, err := m.store.Create(ctx, input)
	if err != nil {
		return Account{}, err
	}
	if account.Enabled {
		if err := m.startAccount(ctx, account); err != nil {
			return account, err
		}
	}
	return account, nil
}

func (m *Manager) Update(ctx context.Context, id string, input UpdateAccount) error {
	before, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := m.store.Update(ctx, id, input); err != nil {
		return err
	}
	after, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if before.Enabled && !after.Enabled {
		return m.stopAccount(id)
	}
	if !before.Enabled && after.Enabled {
		return m.startAccount(ctx, after)
	}
	if before.Enabled && after.Enabled && before.MaxInFlight != after.MaxInFlight {
		if err := m.stopAccount(id); err != nil {
			return err
		}
		return m.startAccount(ctx, after)
	}
	return nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	if err := m.stopAccount(id); err != nil {
		return err
	}
	if err := m.store.Delete(ctx, id); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.restarts, id)
	m.mu.Unlock()
	runtimeDir := filepath.Join(m.config.DataDir, "runtime", id)
	if err := os.RemoveAll(runtimeDir); err != nil {
		return fmt.Errorf("remove account runtime: %w", err)
	}
	return nil
}

func (m *Manager) SyncCredential(ctx context.Context, id, authType string) error {
	home := filepath.Join(m.config.DataDir, "runtime", id)
	userBlob, err := os.ReadFile(filepath.Join(home, ".qoder", ".auth", "user"))
	if err != nil {
		return fmt.Errorf("read qoder user credential: %w", err)
	}
	machineID, err := os.ReadFile(filepath.Join(home, ".qoder", ".auth", "machine_id"))
	if err != nil {
		return fmt.Errorf("read qoder machine id: %w", err)
	}
	return m.store.SaveCredential(ctx, id, authType, NativeCredential{
		UserBlob:  userBlob,
		MachineID: string(machineID),
	})
}

func (m *Manager) stopAccount(id string) error {
	m.mu.Lock()
	process := m.processes[id]
	delete(m.processes, id)
	m.mu.Unlock()
	m.pool.Remove(id)
	if process == nil {
		return nil
	}
	return process.Stop()
}

type ImportAccount struct {
	Name       string
	Provider   string
	Region     string
	Enabled    bool
	Credential NativeCredential
}

type AccountView struct {
	Account
	Ready     bool   `json:"ready"`
	Hot       bool   `json:"hot"`
	InFlight  int    `json:"in_flight"`
	Restarts  int    `json:"restarts"`
	DownUntil string `json:"down_until,omitempty"`
}

func (m *Manager) Import(ctx context.Context, input ImportAccount) (Account, error) {
	account, err := m.store.Create(ctx, CreateAccount{
		Name: input.Name, Provider: input.Provider, Region: input.Region, Enabled: false,
	})
	if err != nil {
		return Account{}, err
	}
	if err := m.store.SaveCredential(ctx, account.ID, "native", input.Credential); err != nil {
		_ = m.store.Delete(ctx, account.ID)
		return Account{}, err
	}
	if input.Enabled {
		enabled := true
		if err := m.store.Update(ctx, account.ID, UpdateAccount{Enabled: &enabled}); err != nil {
			return Account{}, err
		}
		account, err = m.store.Get(ctx, account.ID)
		if err != nil {
			return Account{}, err
		}
		if err := m.startAccount(ctx, account); err != nil {
			return account, err
		}
	}
	return m.store.Get(ctx, account.ID)
}

func (m *Manager) Accounts(ctx context.Context) ([]AccountView, error) {
	stored, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]AccountView, 0, len(stored))
	for _, account := range stored {
		view := AccountView{Account: account}
		if item, ok := m.pool.ByID(account.ID); ok {
			view.Ready = item.Ready == nil || *item.Ready
			view.Hot = item.Hot != nil && *item.Hot
			view.InFlight = item.InFlight
			view.Restarts = item.Restarts
			if !item.DownUntil.IsZero() && time.Now().Before(item.DownUntil) {
				view.DownUntil = item.DownUntil.UTC().Format(time.RFC3339)
			}
			if view.LastError == "" {
				view.LastError = item.LastError
			}
			if view.LastErrorKind == "" {
				view.LastErrorKind = item.LastKind
			}
		}
		views = append(views, view)
	}
	return views, nil
}

func (m *Manager) AccountURL(id string) (string, bool) {
	item, ok := m.pool.ByID(id)
	return item.URL, ok
}

func (m *Manager) RefreshAll(ctx context.Context) error {
	var joined error
	for _, item := range m.pool.Items() {
		if err := m.refreshOne(ctx, item); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (m *Manager) refreshOne(ctx context.Context, item Item) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		ready := false
		hot := false
		m.pool.MergeHealth(item.ID, ready, hot, 0, item.Restarts, err.Error())
		_ = m.store.Observe(ctx, item.ID, "", "error", err.Error(), KindUnavailable)
		return fmt.Errorf("health account %s: %w", item.ID, err)
	}
	defer resp.Body.Close()
	var health struct {
		OK        bool   `json:"ok"`
		Ready     bool   `json:"ready"`
		Hot       bool   `json:"hot"`
		UID       string `json:"uid"`
		InFlight  int    `json:"inFlight"`
		LastError string `json:"lastError"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return fmt.Errorf("decode account %s health: %w", item.ID, err)
	}
	ready := resp.StatusCode < 300 && health.OK && health.Ready
	m.pool.MergeHealth(item.ID, ready, health.Hot, health.InFlight, item.Restarts, health.LastError)
	status := "login_required"
	if ready || health.Hot {
		status = "ready"
	} else if health.LastError != "" {
		status = "error"
	}
	if err := m.store.Observe(ctx, item.ID, health.UID, status, health.LastError, ""); err != nil {
		return err
	}
	return nil
}

func (m *Manager) watchAccount(id string, process ManagedProcess) {
	var exitErr error
	select {
	case exitErr = <-process.Done():
	case <-m.runCtx.Done():
		return
	}
	m.mu.Lock()
	if m.processes[id] != process {
		m.mu.Unlock()
		return
	}
	delete(m.processes, id)
	m.restarts[id]++
	m.mu.Unlock()
	m.pool.Remove(id)
	message := "account daemon exited"
	if exitErr != nil {
		message = exitErr.Error()
	}
	_ = m.store.Observe(context.Background(), id, "", "error", message, KindUnavailable)
	select {
	case <-time.After(m.config.RestartDelay):
	case <-m.runCtx.Done():
		return
	}
	account, err := m.store.Get(context.Background(), id)
	if err != nil || !account.Enabled {
		return
	}
	_ = m.startAccount(context.Background(), account)
}
