package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type runnerStep struct {
	name          string
	args          []string
	output        []byte
	err           error
	requireActive bool
	beforeReturn  func()
}

type scriptedRunner struct {
	t     *testing.T
	steps []runnerStep
	index int
}

func (r *scriptedRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.t.Helper()
	if r.index >= len(r.steps) {
		r.t.Fatalf("unexpected command: %s %s", name, strings.Join(args, " "))
	}
	step := r.steps[r.index]
	r.index++
	if name != step.name || !reflect.DeepEqual(args, step.args) {
		r.t.Fatalf("command %d = %s %q, want %s %q", r.index, name, args, step.name, step.args)
	}
	if step.requireActive && ctx.Err() != nil {
		r.t.Fatalf("command %d received canceled context: %v", r.index, ctx.Err())
	}
	if step.beforeReturn != nil {
		step.beforeReturn()
	}
	return step.output, step.err
}

func (r *scriptedRunner) assertDone() {
	r.t.Helper()
	if r.index != len(r.steps) {
		r.t.Fatalf("executed %d commands, want %d", r.index, len(r.steps))
	}
}

func TestExecutorApplySuccess(t *testing.T) {
	directory := t.TempDir()
	envPath := filepath.Join(directory, ".env")
	composePath := filepath.Join(directory, "docker-compose.yml")
	if err := os.WriteFile(envPath, []byte("QODER_MAX_INFLIGHT=4\nCLI2API_IMAGE=old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	const (
		currentVersion = "v0.2.1"
		targetVersion  = "v0.2.2"
		backupPath     = "/data/backups/qoder-20260824T010203.000000000Z.db"
		repository     = "ghcr.io/caigee-cmd/cli2api"
	)
	targetImage := repository + ":" + targetVersion
	currentImage := repository + ":" + currentVersion
	inspectBefore := inspectOutputWithNetworks("sha256:old", "qoder-data", map[string][]string{
		"deploy_default":                 {"qoder-api-proxy", "qoder-api-proxy"},
		"sub2api-deploy_sub2api-network": {"qoder-api-proxy"},
	})
	inspectAfter := inspectOutputWithNetworks("sha256:new", "qoder-data", map[string][]string{
		"deploy_default": {"qoder-api-proxy", "qoder-api-proxy"},
	})

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"version":%q}`, targetVersion)
	}))
	defer health.Close()

	runner := &scriptedRunner{t: t, steps: []runnerStep{
		{name: "docker", args: []string{"inspect", "qoder-api-proxy"}, output: inspectBefore},
		{name: "docker", args: []string{"image", "tag", "sha256:old", currentImage}},
		{name: "docker", args: []string{"run", "--rm", "-v", "qoder-data:/data", "-e", "BACKUP_PATH=" + backupPath, "--entrypoint", "/bin/sh", currentImage, "-c", `test -f "$BACKUP_PATH"`}},
		{name: "docker", args: []string{"pull", targetImage}},
		{name: "docker", args: composeArgs(envPath, composePath, "up", "-d", "--no-deps", "--force-recreate", "qoder-api-proxy")},
		{name: "docker", args: []string{"inspect", "qoder-api-proxy"}, output: inspectAfter},
		{name: "docker", args: []string{"network", "connect", "--alias", "qoder-api-proxy", "sub2api-deploy_sub2api-network", "qoder-api-proxy"}},
	}}
	executor := NewExecutor(ExecutorConfig{
		ComposeFile: composePath, EnvFile: envPath, ImageRepository: repository,
		HealthURL: health.URL, HealthTimeout: time.Second,
	})
	executor.runner = runner

	var states []string
	rolledBack, err := executor.Apply(context.Background(), "job-success", ApplyRequest{
		CurrentVersion: currentVersion,
		TargetVersion:  targetVersion,
		BackupPath:     backupPath,
	}, func(state string) {
		states = append(states, state)
	})
	if err != nil {
		t.Fatal(err)
	}
	if rolledBack {
		t.Fatal("successful update reported rollback")
	}
	if want := []string{"preparing", "pulling", "recreating", "checking"}; !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CLI2API_IMAGE="+targetImage) {
		t.Fatalf("env = %q", data)
	}
	runner.assertDone()
}

func TestExecutorRollbackUsesFreshContextAndOldImage(t *testing.T) {
	directory := t.TempDir()
	envPath := filepath.Join(directory, ".env")
	composePath := filepath.Join(directory, "docker-compose.yml")
	originalEnv := []byte("QODER_MAX_INFLIGHT=4\nCLI2API_IMAGE=old\n")
	if err := os.WriteFile(envPath, originalEnv, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(composePath, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	const (
		currentVersion = "v0.2.1"
		targetVersion  = "v0.2.2"
		backupPath     = "/data/backups/qoder-20260824T010203.000000000Z.db"
		repository     = "ghcr.io/caigee-cmd/cli2api"
	)
	targetImage := repository + ":" + targetVersion
	currentImage := repository + ":" + currentVersion
	applyCtx, cancelApply := context.WithCancel(context.Background())
	defer cancelApply()

	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"ok":true,"version":%q}`, currentVersion)
	}))
	defer health.Close()

	runner := &scriptedRunner{t: t, steps: []runnerStep{
		{name: "docker", args: []string{"inspect", "qoder-api-proxy"}, output: inspectOutputWithNetworks("sha256:old", "qoder-data", map[string][]string{
			"deploy_default":                 {"qoder-api-proxy"},
			"sub2api-deploy_sub2api-network": {"qoder-api-proxy"},
		})},
		{name: "docker", args: []string{"image", "tag", "sha256:old", currentImage}},
		{name: "docker", args: []string{"run", "--rm", "-v", "qoder-data:/data", "-e", "BACKUP_PATH=" + backupPath, "--entrypoint", "/bin/sh", currentImage, "-c", `test -f "$BACKUP_PATH"`}},
		{name: "docker", args: []string{"pull", targetImage}},
		{name: "docker", args: composeArgs(envPath, composePath, "up", "-d", "--no-deps", "--force-recreate", "qoder-api-proxy"), err: errors.New("recreate failed"), beforeReturn: cancelApply},
		{name: "docker", args: composeArgs(envPath, composePath, "stop", "qoder-api-proxy"), requireActive: true},
		{name: "docker", args: []string{"run", "--rm", "-v", "qoder-data:/data", "-e", "BACKUP_PATH=" + backupPath, "--entrypoint", "/bin/sh", currentImage, "-c", restoreSQLiteScript()}, requireActive: true},
		{name: "docker", args: composeArgs(envPath, composePath, "up", "-d", "--no-deps", "--force-recreate", "qoder-api-proxy"), requireActive: true},
		{name: "docker", args: []string{"inspect", "qoder-api-proxy"}, output: inspectOutputWithNetworks("sha256:old", "qoder-data", map[string][]string{
			"deploy_default": {"qoder-api-proxy"},
		}), requireActive: true},
		{name: "docker", args: []string{"network", "connect", "--alias", "qoder-api-proxy", "sub2api-deploy_sub2api-network", "qoder-api-proxy"}, requireActive: true},
	}}
	executor := NewExecutor(ExecutorConfig{
		ComposeFile: composePath, EnvFile: envPath, ImageRepository: repository,
		HealthURL: health.URL, HealthTimeout: time.Second,
	})
	executor.runner = runner

	var states []string
	rolledBack, err := executor.Apply(applyCtx, "job-rollback", ApplyRequest{
		CurrentVersion: currentVersion,
		TargetVersion:  targetVersion,
		BackupPath:     backupPath,
	}, func(state string) {
		states = append(states, state)
	})
	if err == nil || !strings.Contains(err.Error(), "was rolled back") {
		t.Fatalf("error = %v", err)
	}
	if !rolledBack {
		t.Fatal("rollback was not reported")
	}
	if want := []string{"preparing", "pulling", "recreating", "rolling_back"}; !reflect.DeepEqual(states, want) {
		t.Fatalf("states = %v, want %v", states, want)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "CLI2API_IMAGE="+currentImage) {
		t.Fatalf("env = %q, want pinned current image %q", data, currentImage)
	}
	runner.assertDone()
}

func TestSetEnvValueAtomicPreservesExistingSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("QODER_MAX_INFLIGHT=4\nCLI2API_IMAGE=old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original, mode, err := readEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := setEnvValueAtomic(path, mode, "CLI2API_IMAGE", "ghcr.io/caigee-cmd/cli2api:v0.2.2"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "QODER_MAX_INFLIGHT=4") || !strings.Contains(text, "CLI2API_IMAGE=ghcr.io/caigee-cmd/cli2api:v0.2.2") {
		t.Fatalf("env = %q", text)
	}
	if err := writeEnvFileAtomic(path, mode, original); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Fatalf("restored env = %q", restored)
	}
}

func TestExecutorConfigRequiresAbsoluteFiles(t *testing.T) {
	for _, config := range []ExecutorConfig{
		{ComposeFile: "deploy/docker-compose.yml", EnvFile: "/tmp/.env"},
		{ComposeFile: "/tmp/docker-compose.yml", EnvFile: "deploy/.env"},
	} {
		if err := NewExecutor(config).validateConfig(); err == nil {
			t.Fatalf("config unexpectedly accepted: %+v", config)
		}
	}
}

func TestValidateApplyRequestRejectsUnsafeInput(t *testing.T) {
	valid := ApplyRequest{CurrentVersion: "v0.2.1", TargetVersion: "v0.2.2", BackupPath: "/data/backups/qoder-20260824T010203.000000000Z.db"}
	if err := validateApplyRequest(valid); err != nil {
		t.Fatal(err)
	}
	for _, request := range []ApplyRequest{
		{CurrentVersion: "v0.2.1", TargetVersion: "latest", BackupPath: valid.BackupPath},
		{CurrentVersion: "v0.2.2", TargetVersion: "v0.2.1", BackupPath: valid.BackupPath},
		{CurrentVersion: "v0.2.1", TargetVersion: "v0.2.2", BackupPath: "/etc/passwd"},
		{CurrentVersion: "v0.2.1", TargetVersion: "v0.2.2", BackupPath: "/data/backups/../qoder.db"},
	} {
		if err := validateApplyRequest(request); err == nil {
			t.Fatalf("request unexpectedly accepted: %+v", request)
		}
	}
}

func inspectOutput(image, volume string) []byte {
	return inspectOutputWithNetworks(image, volume, nil)
}

func inspectOutputWithNetworks(image, volume string, networks map[string][]string) []byte {
	payload := []map[string]any{{
		"Image": image,
		"Mounts": []map[string]string{{
			"Type": "volume", "Name": volume,
			"Source": "/var/lib/docker/volumes/" + volume + "/_data", "Destination": "/data",
		}},
		"NetworkSettings": map[string]any{"Networks": networkPayload(networks)},
	}}
	data, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return data
}

func networkPayload(networks map[string][]string) map[string]any {
	payload := make(map[string]any, len(networks))
	for name, aliases := range networks {
		payload[name] = map[string]any{"Aliases": aliases}
	}
	return payload
}

func composeArgs(envPath, composePath string, args ...string) []string {
	result := []string{"compose", "--env-file", envPath, "-f", composePath}
	return append(result, args...)
}

func restoreSQLiteScript() string {
	return `set -eu
test -f "$BACKUP_PATH"
cp "$BACKUP_PATH" /data/qoder.db.restore
chmod 600 /data/qoder.db.restore
mv /data/qoder.db.restore /data/qoder.db
rm -f /data/qoder.db-wal /data/qoder.db-shm /data/qoder.db-journal
sync`
}
