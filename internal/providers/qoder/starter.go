package qoder

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	proxyutil "github.com/caigee-cmd/cli2api/internal/proxy"
)

type Process interface {
	URL() string
	Done() <-chan error
	Stop() error
}

type StarterConfig struct {
	NodeBinary     string
	DaemonPath     string
	QoderCLIPath   string
	QoderCNCLIPath string
	TemplatePath   string
	ProxyAPIKey    string
	ProxyURL       string
	MaxLogWriters  io.Writer
}

type Starter struct {
	mu     sync.RWMutex
	Config StarterConfig
}

func (s *Starter) ConfigSnapshot() StarterConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Config
}

func (s *Starter) SetProxyURL(value string) {
	s.mu.Lock()
	s.Config.ProxyURL = strings.TrimSpace(value)
	s.mu.Unlock()
}

func (s *Starter) SetProxyAPIKey(value string) {
	s.mu.Lock()
	s.Config.ProxyAPIKey = value
	s.mu.Unlock()
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

func proxyEnv(env []string, raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return env
	}
	filtered := make([]string, 0, len(env)+2)
	for _, value := range env {
		key := strings.SplitN(value, "=", 2)[0]
		switch strings.ToUpper(key) {
		case "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY":
			continue
		}
		filtered = append(filtered, value)
	}
	if setting, err := proxyutil.Parse(raw); err == nil && setting.Mode == proxyutil.ModeProxy {
		filtered = append(filtered, "HTTP_PROXY="+raw, "HTTPS_PROXY="+raw, "http_proxy="+raw, "https_proxy="+raw)
	}
	return filtered
}

func StarterEnv(config StarterConfig, account accounts.Account, home string, port int) ([]string, error) {
	if config.DaemonPath == "" {
		return nil, fmt.Errorf("worker daemon path required")
	}
	spec, err := RuntimeSpec(config.QoderCLIPath, config.QoderCNCLIPath, account.ProviderRegion, home)
	if err != nil {
		return nil, err
	}
	effectiveProxy := proxyutil.Effective(account.ProxyURL, config.ProxyURL)
	env := proxyEnv(os.Environ(), effectiveProxy)
	return append(env,
		"HOME="+home,
		"QODER_HOME="+spec.ConfigDir,
		spec.ConfigEnv+"="+spec.ConfigDir,
		"QODER_SITE="+spec.Site,
		"QODER_ACCOUNT_ID="+account.ID,
		"QODER_MAX_INFLIGHT="+strconv.Itoa(account.MaxInFlight),
		"WORKER_HOST=127.0.0.1",
		"WORKER_PORT="+strconv.Itoa(port),
		"PROXY_API_KEY="+config.ProxyAPIKey,
		"QODERCLI_JS="+spec.CLIPath,
		"PLAIN_TEMPLATE_PATH="+config.TemplatePath,
		"QODER_WARMUP_CWD="+filepath.Join(home, "work"),
		"QODER_PROXY_URL="+effectiveProxy,
	), nil
}

func (s *Starter) Start(_ context.Context, account accounts.Account, home string, port int) (Process, error) {
	config := s.ConfigSnapshot()
	env, err := StarterEnv(config, account, home, port)
	if err != nil {
		return nil, err
	}
	node := config.NodeBinary
	if node == "" {
		node = "node"
	}
	cmd := exec.Command(node, config.DaemonPath)
	cmd.Env = env
	writer := config.MaxLogWriters
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
