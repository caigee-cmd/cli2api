package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Store loads Qoder local auth material.
// Phase A: we only inspect files and surface what is available.
// Phase B+: decrypt/refresh and inject upstream headers.
type Store struct {
	Home string
	PAT  string
}

type Snapshot struct {
	Home            string `json:"home"`
	HasPAT          bool   `json:"has_pat"`
	HasUserBlob     bool   `json:"has_user_blob"`
	UserBlobBytes   int    `json:"user_blob_bytes"`
	HasMachineID    bool   `json:"has_machine_id"`
	MachineID       string `json:"machine_id,omitempty"`
	EndpointCache   any    `json:"endpoint_cache,omitempty"`
	Notes           string `json:"notes"`
}

func (s Store) Snapshot() (Snapshot, error) {
	out := Snapshot{
		Home:   s.Home,
		HasPAT: s.PAT != "",
		Notes:  "OAuth user blob appears encrypted; PAT path preferred if Global supports it.",
	}
	userPath := filepath.Join(s.Home, ".auth", "user")
	if b, err := os.ReadFile(userPath); err == nil {
		out.HasUserBlob = true
		out.UserBlobBytes = len(b)
	}
	midPath := filepath.Join(s.Home, ".auth", "machine_id")
	if b, err := os.ReadFile(midPath); err == nil {
		out.HasMachineID = true
		out.MachineID = string(b)
	}
	cachePath := filepath.Join(s.Home, ".cache", "endpoint-cache.json")
	if b, err := os.ReadFile(cachePath); err == nil {
		var raw any
		if json.Unmarshal(b, &raw) == nil {
			out.EndpointCache = raw
		}
	}
	if !out.HasPAT && !out.HasUserBlob {
		return out, fmt.Errorf("no Qoder auth found under %s and no QODER_PERSONAL_ACCESS_TOKEN", s.Home)
	}
	return out, nil
}
