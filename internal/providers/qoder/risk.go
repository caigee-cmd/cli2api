package qoder

// Real device risk fingerprint for the Qoder campaign (check-in) API.
//
// The campaign service only returns the claimable "每天领 100 Credits" activity
// to clients that present the SAME machine fingerprint the Qoder desktop app
// generates. A PAT-derived or random fingerprint is treated as an unofficial
// client and the daily-credit campaign is silently dropped from the response
// (only promo activities remain). The genuine fingerprint comes from the
// `umid/runtime-info` helper shipped inside the desktop app, which prints a
// JSON object with machineToken/machineType/machineCode.
//
// We therefore locate that helper in the installed app and run it. When it is
// unavailable (headless server, different OS, etc.) we fall back to the derived
// identity so the call still works — it just may not see the daily campaign.
//
// Ported from the qoder2api reference project (Qoder-api/auth/risk.go).

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// RiskIdentity is the desktop device fingerprint the campaign service expects.
type RiskIdentity struct {
	MachineToken string
	MachineType  string
	MachineCode  string
}

// Valid reports whether all three fingerprint fields are present. A partial
// identity is treated as an unofficial client, so callers must check this.
func (r *RiskIdentity) Valid() bool {
	return r != nil && r.MachineToken != "" && r.MachineType != "" && r.MachineCode != ""
}

// runtimeInfoCandidates lists the known install locations of the umid helper.
func runtimeInfoCandidates() []string {
	var out []string
	switch runtime.GOOS {
	case "darwin":
		out = append(out,
			"/Applications/Qoder CN.app/Contents/Resources/umid/runtime-info",
			"/Applications/Qoder.app/Contents/Resources/umid/runtime-info",
		)
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			out = append(out,
				filepath.Join(local, "Programs", "Qoder CN", "resources", "umid", "runtime-info.exe"),
				filepath.Join(local, "Programs", "Qoder", "resources", "umid", "runtime-info.exe"),
			)
		}
	}
	// Optional explicit override (tests, custom installs).
	if p := strings.TrimSpace(os.Getenv("QODER_RUNTIME_INFO")); p != "" {
		out = append([]string{p}, out...)
	}
	return out
}

// findRuntimeInfo returns the first existing helper path, or "".
func findRuntimeInfo() string {
	for _, p := range runtimeInfoCandidates() {
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// riskIdentityTimeout bounds the helper invocation so a hung binary cannot
// stall a check-in.
var riskIdentityTimeout = 5 * time.Second

// readRiskIdentityExec runs the helper and parses its JSON. Split out so tests
// can substitute a fake executable.
func readRiskIdentityExec(ctx context.Context, exe string) *RiskIdentity {
	runCtx, cancel := context.WithTimeout(ctx, riskIdentityTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, exe)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	line := out
	if i := strings.IndexByte(string(out), '\n'); i >= 0 {
		line = out[:i]
	}
	var payload struct {
		MachineToken string `json:"machineToken"`
		MachineType  string `json:"machineType"`
		MachineCode  string `json:"machineCode"`
	}
	if err := json.Unmarshal(line, &payload); err != nil {
		return nil
	}
	id := &RiskIdentity{
		MachineToken: strings.TrimSpace(payload.MachineToken),
		MachineType:  strings.TrimSpace(payload.MachineType),
		MachineCode:  strings.TrimSpace(payload.MachineCode),
	}
	if !id.Valid() {
		return nil
	}
	return id
}

// ReadRiskIdentity locates the Qoder umid helper and returns the genuine device
// fingerprint. Returns nil when the helper is missing or unusable.
func ReadRiskIdentity(ctx context.Context) *RiskIdentity {
	exe := findRuntimeInfo()
	if exe == "" {
		return nil
	}
	return readRiskIdentityExec(ctx, exe)
}
