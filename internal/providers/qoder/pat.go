package qoder

import (
	"context"
	"crypto/md5"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/providers/qoder/qauth"
)

// PATCredentialFormat is the stored-credential format identifier for a Qoder
// Personal Access Token. The PAT is the long-lived credential; the bridge
// exchanges it for a short-lived securityOauthToken + refreshToken via
// qauth.ExchangeJobToken at bootstrap, and rotates it via qauth.RefreshJobToken
// before expiry. The legacy qoder-native-v1 format (the encrypted CLI user
// blob) is left in the descriptor for backward compatibility so old accounts
// show a "re-login required" state instead of being unreadable.
const PATCredentialFormat = "qoder-pat-v1"

// patCodec validates and imports a pasted Qoder PAT. The console LoginPAT
// path wraps the pasted token as {"api_key": "<PAT>"}; the codec unwraps it,
// validates the shape, and returns the raw PAT bytes as the stored payload —
// the bridge reads them back via LoadCredentialPayload at request time.
type patCodec struct{}

func (patCodec) Format() string { return PATCredentialFormat }

// Validate accepts a {"api_key": "<PAT>"} envelope and checks the token shape.
// Qoder PATs are opaque ASCII strings; the only hard constraint is that the
// field is present and non-empty after trimming. Length bounds reject obvious
// paste errors (a full auth blob or a URL).
func (patCodec) Validate(payload []byte) error {
	pat, err := unwrapPATEvelope(payload)
	if err != nil {
		return err
	}
	if len(pat) < 16 || len(pat) > 512 {
		return errors.New("qoder PAT has invalid length")
	}
	return nil
}

// PrepareImport validates the pasted envelope and returns the canonical
// payload (the raw PAT bytes) for SaveCredentialPayload. Ready is true: a PAT
// is a complete credential, and the bridge will bootstrap the session on the
// first request, so the account may be enabled immediately.
func (patCodec) PrepareImport(payload []byte) (providers.CredentialImport, error) {
	pat, err := unwrapPATEvelope(payload)
	if err != nil {
		return providers.CredentialImport{}, err
	}
	if err := (patCodec{}).Validate(payload); err != nil {
		return providers.CredentialImport{}, err
	}
	return providers.CredentialImport{Payload: []byte(pat), Ready: true}, nil
}

// unwrapPATEvelope pulls the api_key string out of the {"api_key": ...} JSON
// envelope the console LoginPAT path sends. A bare string is also accepted so
// direct programmatic callers do not need the envelope.
func unwrapPATEvelope(payload []byte) (string, error) {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return "", errors.New("qoder PAT is empty")
	}
	// Bare token (no JSON envelope).
	if !strings.HasPrefix(trimmed, "{") {
		return trimmed, nil
	}
	var envelope struct {
		APIKey string `json:"api_key"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return "", errors.New("qoder PAT envelope is invalid JSON")
	}
	pat := strings.TrimSpace(envelope.APIKey)
	if pat == "" {
		return "", errors.New("qoder PAT envelope has empty api_key")
	}
	return pat, nil
}

// LoadPAT reads the stored PAT payload bytes back as a string. Returns empty
// string + a sentinel error when the account has no PAT (legacy
// qoder-native-v1 blob only — the console should mark it for re-login).
func LoadPAT(format string, payload []byte) (string, error) {
	if format != PATCredentialFormat {
		return "", ErrPATRequired
	}
	pat := strings.TrimSpace(string(payload))
	if pat == "" {
		return "", ErrPATRequired
	}
	return pat, nil
}

// ErrPATRequired marks a Qoder account that predates the PAT credential model
// (it has only the legacy qoder-native-v1 CLI blob, which cannot be reversed
// back into a PAT). The console surfaces this as "re-login required".
var ErrPATRequired = errors.New("qoder account requires PAT re-login (legacy CLI blob cannot be reversed to a PAT)")

// deriveMachineIdentity derives a stable device fingerprint from a PAT. The
// gateway's per-account admission and the OpenAPI campaigns endpoint both see
// the cosy-machine{Id,Token,Type} headers; deriving them from the PAT (not
// from the uid, which is only known after bootstrap) means the same account
// presents the same device across restarts, PAT re-exchanges, and machine
// restarts. A random per-process identity (the Qoder-2API-Go approach) would mint
// a new device on every rebuild and risks the OpenAPI dropping the
// claimable credit campaign as an unofficial client.
//
// The derivation mirrors cosy.go's uid-based derivation (md5 for the id,
// sha512-trimmed for the token, md5-trimmed for the type) but keyed on the
// PAT so it is available before the first jobToken exchange.
func deriveMachineIdentity(pat string) (machineID, machineToken, machineType string) {
	idSum := md5.Sum([]byte("machine:" + pat))
	machineID = hex.EncodeToString(idSum[:])
	tokSum := sha512.Sum512([]byte("machinetoken:" + pat))
	machineToken = strings.TrimRight(base64.RawURLEncoding.EncodeToString(tokSum[:]), "=")[:43]
	typeSum := md5.Sum([]byte("machinetype:" + pat))
	typeHex := hex.EncodeToString(typeSum[:])
	typeHex = strings.ReplaceAll(typeHex, "-", "")
	if len(typeHex) > 18 {
		typeHex = typeHex[:18]
	}
	machineType = typeHex
	return
}

// patSignature is the cache key for a cachedBridge: it changes whenever the
// stored PAT or region rotates, so the bridge is rebuilt lazily on the next
// request after a credential change.
func patSignature(pat, region string) string {
	return region + ":" + pat
}

// Bridge resolves the in-process Bridge for an account. It loads the stored
// PAT, derives the device identity, and returns the cached Bridge (building
// one on miss). Returns ErrPATRequired when the account predates the PAT
// model and still carries only the legacy CLI blob — the console should mark
// it for re-login and the caller should fall back to the worker path if any.
func (c *Client) Bridge(ctx context.Context, accountID string) (*Bridge, error) {
	c.mu.RLock()
	store := c.patStore
	regionOf := c.regionOf
	c.mu.RUnlock()
	if store == nil {
		// No PAT store wired: behave as a legacy account that the operator
		// has not yet re-logged with a PAT. Callers fall back to the worker
		// path, preserving backward compatibility with stubbed-worker tests
		// and deployments that still run the Node worker.
		return nil, ErrPATRequired
	}
	format, payload, err := store.LoadCredentialPayload(ctx, accountID)
	if err != nil {
		return nil, err
	}
	pat, err := LoadPAT(format, payload)
	if err != nil {
		return nil, err
	}
	region := "global"
	if regionOf != nil {
		if r, err := regionOf(ctx, accountID); err == nil && r != "" {
			region = r
		}
	}
	sig := patSignature(pat, region)
	c.bridgesMu.Lock()
	defer c.bridgesMu.Unlock()
	if cached, ok := c.bridges[accountID]; ok && cached.patSig == sig {
		return cached.bridge, nil
	}
	machineID, machineToken, machineType := deriveMachineIdentity(pat)
	b := NewBridge(pat, qauth.RegionByName(region), machineID, machineToken, machineType)
	if c.bridges == nil {
		c.bridges = make(map[string]*cachedBridge)
	}
	c.bridges[accountID] = &cachedBridge{bridge: b, patSig: sig}
	return b, nil
}

func (c *Client) bridge(ctx context.Context, accountID string) (*Bridge, error) {
	return c.Bridge(ctx, accountID)
}
