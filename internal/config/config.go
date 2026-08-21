package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host       string
	Port       int
	ProxyAPIKey string
	QoderHome  string
	QoderPAT   string
}

func Load() Config {
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
	return Config{
		Host:        host,
		Port:        port,
		ProxyAPIKey: strings.TrimSpace(os.Getenv("PROXY_API_KEY")),
		QoderHome:   home,
		QoderPAT:    firstNonEmpty(os.Getenv("QODER_PERSONAL_ACCESS_TOKEN"), os.Getenv("QODER_PAT")),
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
