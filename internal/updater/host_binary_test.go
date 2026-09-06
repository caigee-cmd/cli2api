package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestStageHostBinaryWritesVerifiedPayload(t *testing.T) {
	directory := t.TempDir()
	hostPath := filepath.Join(directory, "cli2api-updater")
	if err := os.WriteFile(hostPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("new-updater-binary")
	sum := sha256.Sum256(payload)
	asset := hostUpdaterAssetName()
	executor := NewExecutor(ExecutorConfig{HostBinaryPath: hostPath})
	executor.fetch = func(_ context.Context, version, name string) ([]byte, error) {
		if version != "v0.2.47" {
			t.Fatalf("version = %s", version)
		}
		if name == asset {
			return payload, nil
		}
		if name == "cli2api-updater_checksums.txt" {
			return []byte(hex.EncodeToString(sum[:]) + "  " + asset + "\n"), nil
		}
		t.Fatalf("unexpected asset %s", name)
		return nil, nil
	}
	if err := executor.stageHostBinary(context.Background(), "v0.2.47", nil); err != nil {
		t.Fatal(err)
	}
	staged, err := os.ReadFile(hostPath + ".new")
	if err != nil {
		t.Fatal(err)
	}
	if string(staged) != string(payload) {
		t.Fatalf("staged = %q", staged)
	}
}

func TestCommitHostBinaryReplacesCurrentFile(t *testing.T) {
	directory := t.TempDir()
	hostPath := filepath.Join(directory, "cli2api-updater")
	if err := os.WriteFile(hostPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostPath+".new", []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	executor := NewExecutor(ExecutorConfig{HostBinaryPath: hostPath})
	if err := executor.CommitHostBinary(); err != nil {
		t.Fatal(err)
	}
	current, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != "new" {
		t.Fatalf("current = %q", current)
	}
	backup, err := os.ReadFile(hostPath + ".backup")
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != "old" {
		t.Fatalf("backup = %q", backup)
	}
}

func TestVerifyHostBinaryChecksumRejectsMismatch(t *testing.T) {
	err := verifyHostBinaryChecksum("cli2api-updater_linux_amd64", []byte("body"), []byte("deadbeef  cli2api-updater_linux_amd64\n"))
	if err == nil {
		t.Fatal("expected checksum mismatch")
	}
}
