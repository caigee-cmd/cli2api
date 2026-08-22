package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host        string
	Port        int
	ProxyAPIKey string
	QoderHome   string
	QoderPAT    string
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
	return Config{
		Host:        host,
		Port:        port,
		ProxyAPIKey: key,
		QoderHome:   home,
		QoderPAT:    firstNonEmpty(os.Getenv("QODER_PERSONAL_ACCESS_TOKEN"), os.Getenv("QODER_PAT")),
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
