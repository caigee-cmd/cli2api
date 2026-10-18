package qoder

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// readRiskIdentityExec parses a valid helper payload.
func TestReadRiskIdentityExecValid(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "runtime-info")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"machineToken\":\"tok88\",\"machineType\":\"type18\",\"machineCode\":\"code18\",\"vmInfo\":{}}'\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	id := readRiskIdentityExec(context.Background(), exe)
	if !id.Valid() {
		t.Fatalf("expected valid identity, got %+v", id)
	}
	if id.MachineToken != "tok88" || id.MachineType != "type18" || id.MachineCode != "code18" {
		t.Errorf("unexpected identity: %+v", id)
	}
}

// A partial identity (missing a field) is rejected.
func TestReadRiskIdentityExecPartial(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "runtime-info")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"machineToken\":\"tok\",\"machineType\":\"type\"}'\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if id := readRiskIdentityExec(context.Background(), exe); id != nil {
		t.Errorf("partial identity must be rejected, got %+v", id)
	}
}

// A missing executable yields nil.
func TestReadRiskIdentityExecMissing(t *testing.T) {
	if id := readRiskIdentityExec(context.Background(), filepath.Join(t.TempDir(), "nope")); id != nil {
		t.Errorf("missing executable must yield nil, got %+v", id)
	}
}

// QODER_RUNTIME_INFO override is honored by ReadRiskIdentity.
func TestReadRiskIdentityOverride(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "runtime-info")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"machineToken\":\"T\",\"machineType\":\"Y\",\"machineCode\":\"C\"}'\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("QODER_RUNTIME_INFO", exe)
	id := ReadRiskIdentity(context.Background())
	if !id.Valid() || id.MachineToken != "T" {
		t.Fatalf("override not used: %+v", id)
	}
}
