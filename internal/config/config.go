package config

import (
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
	CatalogRefreshInterval time.Duration
}

func Load() (Config, error) {
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
		CatalogRefreshInterval: catalogRefreshInterval(os.Getenv("CLI2API_CATALOG_REFRESH_INTERVAL")),
	}, nil
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
