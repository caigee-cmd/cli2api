package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/caigee-cmd/cli2api/internal/updater"
)

func main() {
	var socketPath string
	var listenAddress string
	var authToken string
	var authTokenFile string
	var statusFile string
	var composeFile string
	var envFile string
	var serviceName string
	var containerName string
	var imageRepository string
	var healthURL string
	var healthTimeout time.Duration
	flag.StringVar(&socketPath, "socket", "/run/cli2api-updater/updater.sock", "Unix socket path")
	flag.StringVar(&listenAddress, "listen", "", "loopback TCP listen address")
	flag.StringVar(&authToken, "auth-token", "", "Bearer token required for TCP mode")
	flag.StringVar(&authTokenFile, "auth-token-file", "", "file containing the TCP Bearer token")
	flag.StringVar(&statusFile, "status-file", "/var/lib/cli2api-updater/status.json", "persistent updater status")
	flag.StringVar(&composeFile, "compose-file", "", "absolute docker-compose.yml path")
	flag.StringVar(&envFile, "env-file", "", "absolute Compose env file path")
	flag.StringVar(&serviceName, "service", "qoder-api-proxy", "Compose service name")
	flag.StringVar(&containerName, "container", "qoder-api-proxy", "container name")
	flag.StringVar(&imageRepository, "image-repository", "ghcr.io/caigee-cmd/cli2api", "allowed image repository")
	flag.StringVar(&healthURL, "health-url", "http://127.0.0.1:3010/health", "service health URL")
	flag.DurationVar(&healthTimeout, "health-timeout", 120*time.Second, "versioned health-check timeout")
	flag.Parse()
	if strings.TrimSpace(authToken) == "" && strings.TrimSpace(authTokenFile) != "" {
		data, err := os.ReadFile(authTokenFile)
		if err != nil {
			log.Fatal(err)
		}
		authToken = strings.TrimSpace(string(data))
	}

	executor := updater.NewExecutor(updater.ExecutorConfig{
		ComposeFile: composeFile, EnvFile: envFile, ServiceName: serviceName,
		ContainerName: containerName, ImageRepository: imageRepository, HealthURL: healthURL,
		HealthTimeout: healthTimeout,
	})
	service := updater.NewService(updater.Config{
		SocketPath: socketPath, ListenAddress: listenAddress, AuthToken: authToken, StatusFile: statusFile,
	}, executor)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := service.Serve(ctx); err != nil {
		log.Fatal(err)
	}
}
