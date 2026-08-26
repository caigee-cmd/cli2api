package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host                   string
	Port                   int
	ProxyAPIKey            string
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
	CrossProviderModelPool bool
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
		Host:              host,
		Port:              port,
		ProxyAPIKey:       "",
		QoderHome:         home,
		DataDir:           dataDir,
		RuntimeDir:        runtimeDir,
		WorkerBasePort:    workerBasePort,
		NodeBinary:        firstNonEmpty(os.Getenv("QODER_NODE_BINARY"), "node"),
		WorkerDaemonPath:  firstNonEmpty(os.Getenv("QODER_WORKER_DAEMON"), "worker/src/daemon.mjs"),
		QoderCLIPath:      firstNonEmpty(os.Getenv("QODERCLI_JS"), "/usr/local/lib/node_modules/@qoder-ai/qodercli/bundle/qodercli.js"),
		QoderCNCLIPath:    firstNonEmpty(os.Getenv("QODERCNCLI_JS"), "/usr/local/lib/node_modules/@qodercn-ai/qoderclicn/bundle/qoderclicn.js"),
		PlainTemplatePath: firstNonEmpty(os.Getenv("PLAIN_TEMPLATE_PATH"), "worker/last-plain.sample.json"),
		UpdateSocketPath:  firstNonEmpty(os.Getenv("UPDATE_SOCKET_PATH"), "/run/cli2api-updater/updater.sock"),
		UpdateAgentURL:    strings.TrimSpace(os.Getenv("UPDATE_AGENT_URL")),
		UpdateAgentToken:  strings.TrimSpace(os.Getenv("UPDATE_AGENT_TOKEN")),
		UpdateGitHubToken: strings.TrimSpace(os.Getenv("UPDATE_GITHUB_TOKEN")),
		CrossProviderModelPool: strings.EqualFold(strings.TrimSpace(os.Getenv("CROSS_PROVIDER_MODEL_POOL")), "1") ||
			strings.EqualFold(strings.TrimSpace(os.Getenv("CROSS_PROVIDER_MODEL_POOL")), "true"),
	}, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
