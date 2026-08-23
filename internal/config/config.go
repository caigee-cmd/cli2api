package config

import (
	"fmt"
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
	key := strings.TrimSpace(os.Getenv("PROXY_API_KEY"))
	allowInsecure := strings.TrimSpace(os.Getenv("ALLOW_INSECURE_API_KEY")) == "1"
	if !allowInsecure && insecureAPIKey(key) {
		return Config{}, fmt.Errorf("PROXY_API_KEY is required; set a real key or ALLOW_INSECURE_API_KEY=1 for local experiments")
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
	return Config{
		Host:              host,
		Port:              port,
		ProxyAPIKey:       key,
		QoderHome:         home,
		DataDir:           dataDir,
		WorkerBasePort:    workerBasePort,
		NodeBinary:        firstNonEmpty(os.Getenv("QODER_NODE_BINARY"), "node"),
		WorkerDaemonPath:  firstNonEmpty(os.Getenv("QODER_WORKER_DAEMON"), "worker/src/daemon.mjs"),
		QoderCLIPath:      firstNonEmpty(os.Getenv("QODERCLI_JS"), "/usr/local/lib/node_modules/-ai/qodercli/bundle/qodercli.js"),
		PlainTemplatePath: firstNonEmpty(os.Getenv("PLAIN_TEMPLATE_PATH"), "worker/last-plain.sample.json"),
	}, nil
}

func insecureAPIKey(key string) bool {
	return key == "" || key == "change-me" || key == "dev-key"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
