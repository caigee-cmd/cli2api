package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// DefaultCatalogRefreshInterval is the documented default for the background
// model-catalog reload. Operators can change it with
// CLI2API_CATALOG_REFRESH_INTERVAL (a Go duration such as 30s or 15m, or plain
// minutes); 0 disables the loop and leaves only manual/console refreshes.
const DefaultCatalogRefreshInterval = 10 * time.Minute

// DefaultUpdateGitHubRepo is the repository whose GitHub releases the console
// follows. It is the fork that publishes this build's container images; set
// UPDATE_GITHUB_REPO to follow another owner/repo instead.
const DefaultUpdateGitHubRepo = "guyu233/cli2api"

func updateGitHubRepo(value string) string {
	repo := strings.TrimSpace(value)
	if repo == "" {
		return DefaultUpdateGitHubRepo
	}
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "github.com/"} {
		repo = strings.TrimPrefix(repo, prefix)
	}
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.Trim(repo, "/")
	if repo == "" {
		return DefaultUpdateGitHubRepo
	}
	return repo
}

type Config struct {
	Host                   string
	Port                   int
	ProxyAPIKey            string
	ProxyURL               string
	MaxRetryAccounts       int
	QoderHome              string
	DataDir                string
	RuntimeDir             string
	WorkerBasePort         int
	NodeBinary             string
	WorkerDaemonPath       string
	QoderCLIPath           string
	QoderCNCLIPath         string
	PlainTemplatePath      string
	UpdateSocketPath       string
	UpdateAgentURL         string
	UpdateAgentToken       string
	UpdateGitHubToken      string
	UpdateGitHubRepo       string
	CatalogRefreshInterval time.Duration
	// CreditFloor is the global default credit floor. Zero disables the gate;
	// per-account credit_floor overrides it when set.
	CreditFloor float64
	// PercentFloor is the global default floor for percent-denominated quota
	// snapshots (remaining percent). Zero disables the percent gate; a
	// per-account percent_floor overrides it when set.
	PercentFloor float64
}

func Load() (Config, error) {
	creditFloor, err := parseFloorEnv("CLI2API_CREDIT_FLOOR", os.Getenv("CLI2API_CREDIT_FLOOR"))
	if err != nil {
		return Config{}, err
	}
	percentFloor, err := parseFloorEnv("CLI2API_PERCENT_FLOOR", os.Getenv("CLI2API_PERCENT_FLOOR"))
	if err != nil {
		return Config{}, err
	}
	port := 3010
	if v := strings.TrimSpace(os.Getenv("PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			port = n
		}
	}
	home := strings.TrimSpace(os.Getenv("QODER_HOME"))
	if home == "" {
		home = "/root/.qoder"
	}
	host := strings.TrimSpace(os.Getenv("HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	workerBasePort := 32100
	if v := strings.TrimSpace(os.Getenv("QODER_WORKER_BASE_PORT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workerBasePort = n
		}
	}
	maxRetryAccounts := 4
	if v := strings.TrimSpace(os.Getenv("QODER_MAX_RETRY_ACCOUNTS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxRetryAccounts = n
		}
	}
	if maxRetryAccounts > 64 {
		maxRetryAccounts = 64
	}
	dataDir := strings.TrimSpace(os.Getenv("QODER_DATA_DIR"))
	if dataDir == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			dataDir = userHome + "/.qoder-api-proxy"
		} else {
			dataDir = "data"
		}
	}
	runtimeDir := strings.TrimSpace(os.Getenv("QODER_RUNTIME_DIR"))
	if runtimeDir == "" {
		runtimeDir = "/tmp/cli2api-runtime"
	}
	return Config{
		Host:                   host,
		Port:                   port,
		ProxyAPIKey:            "",
		ProxyURL:               strings.TrimSpace(os.Getenv("QODER_PROXY_URL")),
		MaxRetryAccounts:       maxRetryAccounts,
		QoderHome:              home,
		DataDir:                dataDir,
		RuntimeDir:             runtimeDir,
		WorkerBasePort:         workerBasePort,
		NodeBinary:             firstNonEmpty(os.Getenv("QODER_NODE_BINARY"), "node"),
		WorkerDaemonPath:       firstNonEmpty(os.Getenv("QODER_WORKER_DAEMON"), "worker/src/daemon.mjs"),
		QoderCLIPath:           firstNonEmpty(os.Getenv("QODERCLI_JS"), "/usr/local/lib/node_modules/@qoder-ai/qodercli/bundle/qodercli.js"),
		QoderCNCLIPath:         firstNonEmpty(os.Getenv("QODERCNCLI_JS"), "/usr/local/lib/node_modules/@qodercn-ai/qoderclicn/bundle/qoderclicn.js"),
		PlainTemplatePath:      firstNonEmpty(os.Getenv("PLAIN_TEMPLATE_PATH"), "worker/last-plain.sample.json"),
		UpdateSocketPath:       firstNonEmpty(os.Getenv("UPDATE_SOCKET_PATH"), "/run/cli2api-updater/updater.sock"),
		UpdateAgentURL:         strings.TrimSpace(os.Getenv("UPDATE_AGENT_URL")),
		UpdateAgentToken:       strings.TrimSpace(os.Getenv("UPDATE_AGENT_TOKEN")),
		UpdateGitHubToken:      strings.TrimSpace(os.Getenv("UPDATE_GITHUB_TOKEN")),
		UpdateGitHubRepo:       updateGitHubRepo(os.Getenv("UPDATE_GITHUB_REPO")),
		CatalogRefreshInterval: catalogRefreshInterval(os.Getenv("CLI2API_CATALOG_REFRESH_INTERVAL")),
		CreditFloor:            creditFloor,
		PercentFloor:           percentFloor,
	}, nil
}

// parseFloorEnv parses a unit-specific floor environment variable. Unset means
// 0 (disabled). Negative or non-numeric values fail closed so a typo cannot
// silently turn the gate off or admit a wrong threshold.
func parseFloorEnv(name, raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}
	floor, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number, got %q", name, value)
	}
	if floor < 0 {
		return 0, fmt.Errorf("%s must be >= 0, got %v", name, floor)
	}
	return floor, nil
}

// catalogRefreshInterval parses CLI2API_CATALOG_REFRESH_INTERVAL: a Go duration
// ("90s", "15m") or a bare number of minutes ("10"). Unset keeps the default,
// a negative or zero value disables the background loop, and garbage falls back
// to the default with a log line rather than failing the boot.
func catalogRefreshInterval(raw string) time.Duration {
	value := strings.TrimSpace(raw)
	if value == "" {
		return DefaultCatalogRefreshInterval
	}
	if d, err := time.ParseDuration(value); err == nil {
		if d < 0 {
			return 0
		}
		return d
	}
	if minutes, err := strconv.Atoi(value); err == nil {
		if minutes < 0 {
			return 0
		}
		return time.Duration(minutes) * time.Minute
	}
	return DefaultCatalogRefreshInterval
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
