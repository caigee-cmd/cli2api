package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host              string
	Port              int
	ProxyAPIKey       string
	QoderHome         string
	DataDir           string
	RuntimeDir        string
	WorkerBasePort    int
	NodeBinary        string
	WorkerDaemonPath  string
	QoderCLIPath      string
	PlainTemplatePath string
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
		PlainTemplatePath: firstNonEmpty(os.Getenv("PLAIN_TEMPLATE_PATH"), "worker/last-plain.sample.json"),
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
