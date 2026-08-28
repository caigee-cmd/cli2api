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
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

type ManagerConfig struct {
	DataDir        string
	BasePort       int
	NodeBinary     string
	DaemonPath     string
	QoderCLIPath   string
	QoderCNCLIPath string
	TemplatePath   string
	ProxyAPIKey    string
	MaxLogWriters  io.Writer
	RestartDelay   time.Duration
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
	providers  *providers.Registry
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

// SetProviders wires optional in-process account probers (WorkBuddy, etc.).
func (m *Manager) SetProviders(registry *providers.Registry) {
	if m == nil {
		return
	}
	m.providers = registry
}

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
			Runtime: string(descriptor.Runtime), DropSystemPrompt: account.DropSystemPrompt,
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
	if err := materializeHome(ctx, m.store, account, home); err != nil {
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

func qoderConfigDirName(region string) string {
	if strings.EqualFold(strings.TrimSpace(region), "cn") {
		return ".qoder-cn"
	}
	return ".qoder"
}

func qoderAuthDir(home, region string) string {
	return filepath.Join(home, qoderConfigDirName(region), ".auth")
}

func materializeHome(ctx context.Context, store *Store, account Account, home string) error {
	authDir := qoderAuthDir(home, account.ProviderRegion)
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		return fmt.Errorf("create account home: %w", err)
	}
	credential, err := store.LoadCredential(ctx, account.ID)
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

type prefixLogWriter struct {
	prefix string
	next   io.Writer
	buf    []byte
}

func (w *prefixLogWriter) Write(p []byte) (int, error) {
	if w == nil || w.next == nil {
		return len(p), nil
	}
	w.buf = append(w.buf, p...)
	for {
		idx := -1
		for i, b := range w.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := append([]byte(nil), w.buf[:idx+1]...)
		w.buf = w.buf[idx+1:]
		if _, err := w.next.Write(append([]byte(w.prefix), line...)); err != nil {
			return len(p), err
		}
	}
	return len(p), nil
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
	cliPath, site, configDir, configEnv, err := qoderRuntimeSpec(s.Config, account, home)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(node, s.Config.DaemonPath)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"QODER_HOME="+configDir,
		configEnv+"="+configDir,
		"QODER_SITE="+site,
		"QODER_ACCOUNT_ID="+account.ID,
		"QODER_MAX_INFLIGHT="+strconv.Itoa(account.MaxInFlight),
		"WORKER_HOST=127.0.0.1",
		"WORKER_PORT="+strconv.Itoa(port),
		"PROXY_API_KEY="+s.Config.ProxyAPIKey,
		"QODERCLI_JS="+cliPath,
		"PLAIN_TEMPLATE_PATH="+s.Config.TemplatePath,
		"QODER_WARMUP_CWD="+filepath.Join(home, "work"),
	)
	writer := s.Config.MaxLogWriters
	if writer == nil {
		writer = os.Stderr
	}
	writer = &prefixLogWriter{prefix: "[account=" + account.ID + "] ", next: writer}
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
	// Request sanitization applies per request, so sync it into the pool
	// without restarting anything.
	if before.DropSystemPrompt != after.DropSystemPrompt {
		m.pool.SetDropSystemPrompt(id, after.DropSystemPrompt)
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

func qoderRuntimeSpec(cfg ManagerConfig, account Account, home string) (cliPath, site, configDir, configEnv string, err error) {
	region := strings.ToLower(strings.TrimSpace(account.ProviderRegion))
	configDir = filepath.Join(home, qoderConfigDirName(region))
	switch region {
	case "", "global":
		cliPath = strings.TrimSpace(cfg.QoderCLIPath)
		if cliPath == "" {
			return "", "", "", "", fmt.Errorf("qoder global CLI path required")
		}
		return cliPath, "global", configDir, "QODER_CONFIG_DIR", nil
	case "cn":
		cliPath = strings.TrimSpace(cfg.QoderCNCLIPath)
		if cliPath == "" {
			return "", "", "", "", fmt.Errorf("qoder CN CLI path required: set QODERCNCLI_JS to @qodercn-ai/qoderclicn bundle/qoderclicn.js")
		}
		return cliPath, "cn", configDir, "QODERCN_CONFIG_DIR", nil
	default:
		return "", "", "", "", fmt.Errorf("unknown qoder region %q", account.ProviderRegion)
	}
}

func (m *Manager) SyncCredential(ctx context.Context, id, authType string) error {
	account, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	home := filepath.Join(m.config.DataDir, "runtime", id)
	authDir := qoderAuthDir(home, account.ProviderRegion)
	userBlob, err := os.ReadFile(filepath.Join(authDir, "user"))
	if err != nil {
		return fmt.Errorf("read qoder user credential: %w", err)
	}
	machineID, err := os.ReadFile(filepath.Join(authDir, "machine_id"))
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
	Ready     bool           `json:"ready"`
	Hot       bool           `json:"hot"`
	InFlight  int            `json:"in_flight"`
	Restarts  int            `json:"restarts"`
	DownUntil string         `json:"down_until,omitempty"`
	Quota     *QuotaSnapshot `json:"quota,omitempty"`
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
			view.Quota = item.Quota
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
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		return m.refreshInProcess(ctx, item)
	}
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
	// Quota is display-only: fetch after the health/observe path so a quota
	// outage never flips account readiness or scheduling state.
	if health.Hot || ready {
		m.fetchQuota(ctx, item.ID, item.URL)
		m.fetchAccountModels(ctx, item)
	}
	return nil
}

func (m *Manager) refreshInProcess(ctx context.Context, item Item) error {
	adapter, ok := m.providers.Get(item.Provider)
	if !ok || adapter.Prober == nil {
		// No prober registered: leave pool state alone and never hit ""+/health.
		return nil
	}
	health, err := adapter.Prober.Probe(ctx, item.ID)
	if err != nil {
		m.pool.MergeHealth(item.ID, false, false, 0, item.Restarts, err.Error())
		_ = m.store.Observe(ctx, item.ID, "", "error", err.Error(), KindUnavailable)
		return fmt.Errorf("probe account %s: %w", item.ID, err)
	}
	m.pool.MergeHealth(item.ID, health.Ready, health.Hot, health.InFlight, item.Restarts, health.LastError)
	status := "login_required"
	if health.Ready || health.Hot {
		status = "ready"
	} else if health.LastError != "" {
		status = "error"
	}
	if err := m.store.Observe(ctx, item.ID, health.UID, status, health.LastError, ""); err != nil {
		return err
	}
	if health.Ready || health.Hot {
		m.fetchProviderQuota(ctx, item.ID, adapter.Prober)
		m.fetchAccountModels(ctx, item)
	}
	return nil
}

func (m *Manager) fetchProviderQuota(ctx context.Context, accountID string, prober providers.AccountProber) {
	if prober == nil {
		return
	}
	info, err := prober.Quota(ctx, accountID)
	if err != nil || info == nil {
		m.pool.MergeQuota(accountID, nil)
		return
	}
	unit := info.Unit
	if unit == "" {
		unit = "credits"
	}
	m.pool.MergeQuota(accountID, &QuotaSnapshot{
		Used:       info.Used,
		Total:      info.Total,
		Remaining:  info.Remaining,
		Percentage: info.Percentage,
		Unit:       unit,
		Exceeded:   info.Exceeded,
		FetchedAt:  info.FetchedAt,
	})
}

const modelCatalogTTL = 5 * time.Minute

// EnsureModelCatalogs refreshes per-account catalogs that are missing or
// older than the worker TTL. Failures leave the previous snapshot in place
// and never flip readiness or cooldown.
func (m *Manager) EnsureModelCatalogs(ctx context.Context, force bool) {
	if m == nil || m.pool == nil {
		return
	}
	now := time.Now()
	for _, item := range m.pool.Items() {
		if !force && item.Models != nil && !item.ModelsAt.IsZero() && now.Sub(item.ModelsAt) < modelCatalogTTL {
			continue
		}
		m.fetchAccountModels(ctx, item)
	}
}

func (m *Manager) fetchAccountModels(ctx context.Context, item Item) {
	if m == nil || m.pool == nil || item.ID == "" {
		return
	}
	if item.Runtime == string(providers.RuntimeInProcess) || strings.TrimSpace(item.URL) == "" {
		m.fetchProviderModels(ctx, item)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(item.URL, "/")+"/admin/models", nil)
	if err != nil {
		return
	}
	if m.config.ProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.ProxyAPIKey)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return
	}
	var parsed struct {
		Data []map[string]any `json:"data"`
	}
	if json.NewDecoder(resp.Body).Decode(&parsed) != nil {
		return
	}
	m.pool.MergeModels(item.ID, catalogIDs(parsed.Data, nil))
}

func (m *Manager) fetchProviderModels(ctx context.Context, item Item) {
	if m.providers == nil {
		return
	}
	adapter, ok := m.providers.Get(item.Provider)
	if !ok || adapter.Models == nil {
		return
	}
	models, err := adapter.Models.Models(ctx, item.ID)
	if err != nil {
		return
	}
	ids := make([]string, 0, len(models)*2)
	for _, model := range models {
		ids = append(ids, model.PublicModel, model.NativeModel, model.DisplayName)
	}
	m.pool.MergeModels(item.ID, ids)
}

func catalogIDs(entries []map[string]any, extras []string) []string {
	ids := append([]string{}, extras...)
	for _, entry := range entries {
		for _, key := range []string{"id", "mapped_key", "native_model", "display_name"} {
			value, _ := entry[key].(string)
			if strings.TrimSpace(value) != "" {
				ids = append(ids, value)
			}
		}
	}
	return ids
}

// fetchQuota pulls the account quota snapshot from the worker daemon. Errors
// only clear the displayed quota; they are not surfaced as account errors.
func (m *Manager) fetchQuota(ctx context.Context, accountID, workerURL string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, workerURL+"/admin/quota", nil)
	if err != nil {
		return
	}
	if m.config.ProxyAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+m.config.ProxyAPIKey)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return
	}
	var payload struct {
		Quota *workerQuota `json:"quota"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil || payload.Quota == nil {
		return
	}
	m.pool.MergeQuota(accountID, payload.Quota.snapshot())
}

// workerQuota mirrors the daemon /admin/quota response shape.
type workerQuota struct {
	UserQuota          *workerQuotaBlock `json:"userQuota"`
	AddOnQuota         *workerQuotaBlock `json:"addOnQuota"`
	OrgResourcePackage *workerQuotaBlock `json:"orgResourcePackage"`
	IsQuotaExceeded    bool              `json:"isQuotaExceeded"`
	FetchedAt          string            `json:"fetchedAt"`
}

type workerQuotaBlock struct {
	Total      float64 `json:"total"`
	Used       float64 `json:"used"`
	Remaining  float64 `json:"remaining"`
	Percentage float64 `json:"percentage"`
	Unit       string  `json:"unit"`
}

func (w *workerQuota) snapshot() *QuotaSnapshot {
	if w == nil || w.UserQuota == nil {
		return nil
	}
	snapshot := &QuotaSnapshot{
		Used:       w.UserQuota.Used,
		Total:      w.UserQuota.Total,
		Remaining:  w.UserQuota.Remaining,
		Percentage: w.UserQuota.Percentage,
		Unit:       w.UserQuota.Unit,
		Exceeded:   w.IsQuotaExceeded || w.UserQuota.Percentage >= 100,
		FetchedAt:  w.FetchedAt,
	}
	if snapshot.Unit == "" {
		snapshot.Unit = "credits"
	}
	if w.AddOnQuota != nil {
		snapshot.HasAddOn = true
		snapshot.AddOnUsed = w.AddOnQuota.Used
		snapshot.AddOnTotal = w.AddOnQuota.Total
		snapshot.AddOnUnit = w.AddOnQuota.Unit
		if snapshot.AddOnUnit == "" {
			snapshot.AddOnUnit = "credits"
		}
	}
	return snapshot
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
