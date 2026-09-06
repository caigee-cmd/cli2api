package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const maxHostBinaryBytes = 64 << 20

func (e *Executor) stageHostBinary(ctx context.Context, version string, progress func(string)) error {
	hostPath := strings.TrimSpace(e.config.HostBinaryPath)
	if hostPath == "" {
		return nil
	}
	if progress != nil {
		progress("host_binary")
	}
	asset := hostUpdaterAssetName()
	body, err := e.downloadReleaseFile(ctx, version, asset)
	if err != nil {
		return fmt.Errorf("download host updater: %w", err)
	}
	if len(body) == 0 || len(body) > maxHostBinaryBytes {
		return fmt.Errorf("host updater is empty or too large")
	}
	checksums, err := e.downloadReleaseFile(ctx, version, "cli2api-updater_checksums.txt")
	if err != nil {
		return fmt.Errorf("download host updater checksums: %w", err)
	}
	if err := verifyHostBinaryChecksum(asset, body, checksums); err != nil {
		return err
	}
	staged := hostPath + ".new"
	if err := os.WriteFile(staged, body, 0o755); err != nil {
		return fmt.Errorf("write staged host updater: %w", err)
	}
	return nil
}

func (e *Executor) CommitHostBinary() error {
	hostPath := strings.TrimSpace(e.config.HostBinaryPath)
	if hostPath == "" {
		return nil
	}
	staged := hostPath + ".new"
	if _, err := os.Stat(staged); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	backup := hostPath + ".backup"
	_ = os.Remove(backup)
	if _, err := os.Stat(hostPath); err == nil {
		if err := os.Rename(hostPath, backup); err != nil {
			return fmt.Errorf("backup host updater: %w", err)
		}
	}
	if err := os.Rename(staged, hostPath); err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		if restoreErr := os.Rename(backup, hostPath); restoreErr != nil && !os.IsNotExist(restoreErr) {
			return fmt.Errorf("replace host updater: %w (restore error: %v)", err, restoreErr)
		}
		return fmt.Errorf("replace host updater: %w", err)
	}
	return nil
}

func (e *Executor) RestartHost() {
	if len(e.config.RestartCommand) > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, e.config.RestartCommand[0], e.config.RestartCommand[1:]...).Run()
		return
	}
	if runtime.GOOS == "windows" {
		hostPath := strings.TrimSpace(e.config.HostBinaryPath)
		script := `ping 127.0.0.1 -n 3 >nul`
		if hostPath != "" {
			script += fmt.Sprintf(` & if exist "%s.new" move /Y "%s.new" "%s"`, hostPath, hostPath, hostPath)
		}
		script += ` & schtasks /Run /TN "CLI2API Updater"`
		_ = exec.Command("cmd.exe", "/C", script).Start()
		os.Exit(0)
	}
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
}

func (e *Executor) downloadReleaseFile(ctx context.Context, version, name string) ([]byte, error) {
	if e.fetch != nil {
		return e.fetch(ctx, version, name)
	}
	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", strings.Trim(e.config.GitHubRepo, "/"), version, name)
	if err := validateHostBinaryURL(downloadURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "cli2api-updater")
	req.Header.Set("Accept", "application/octet-stream")
	if token := strings.TrimSpace(e.config.GitHubToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := e.download
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub %s returned %d", name, resp.StatusCode)
	}
	if err := validateHostBinaryURL(resp.Request.URL.String()); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxHostBinaryBytes+1))
}

func validateHostBinaryURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid download URL")
	}
	if parsed.Scheme != "https" || parsed.User != nil {
		return fmt.Errorf("untrusted download URL")
	}
	host := strings.ToLower(parsed.Host)
	if host == "github.com" || strings.HasSuffix(host, ".github.com") || host == "objects.githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com") {
		return nil
	}
	return fmt.Errorf("untrusted download host %s", parsed.Host)
}

func verifyHostBinaryChecksum(asset string, body, checksums []byte) error {
	sum := sha256.Sum256(body)
	actual := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == asset {
			if !strings.EqualFold(fields[0], actual) {
				return fmt.Errorf("host updater checksum mismatch")
			}
			return nil
		}
	}
	return fmt.Errorf("host updater checksum missing for %s", asset)
}

func hostUpdaterAssetName() string {
	name := fmt.Sprintf("cli2api-updater_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
