package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers/qoder/qauth"
)

// stubRiskIdentity replaces the package-level helper lookup for the duration of
// a test so the real Qoder app binary is never executed, and returns a restore
// func.
func stubRiskIdentity(t *testing.T, id *RiskIdentity) {
	t.Helper()
	orig := readRiskIdentity
	readRiskIdentity = func(context.Context) *RiskIdentity { return id }
	t.Cleanup(func() { readRiskIdentity = orig })
}

// TestBridgeCheckinHappyPath stubs the OpenAPI campaigns + claim endpoints
// and verifies the full in-process Go path: EnsureFreshSession is a no-op
// (session pre-populated), GET /me/campaigns lists, POST /claim claims,
// headers match worker/src/checkin.mjs, and the result is success + reward.
func TestBridgeCheckinHappyPath(t *testing.T) {
	// Restore hooks after the test.
	origHook := CheckinEndpointHook
	origClient := checkinHTTPClient
	t.Cleanup(func() {
		CheckinEndpointHook = origHook
		checkinHTTPClient = origClient
	})
	stubRiskIdentity(t, nil)

	var gotHeaders http.Header
	var campaignsHits, claimHits atomic.Int32
	openapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/sash/api/v1/me/campaigns":
			campaignsHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"campaigns": []map[string]any{
					{
						"campaignId":  "daily-credits-2026-10-18",
						"actionType":  "CLAIM_BENEFIT",
						"claimStatus": "CLAIMABLE",
						"benefit":     map[string]any{"kind": "CREDITS", "amount": 50.0},
					},
					// A non-credit benefit must be ignored, not claimed.
					{
						"campaignId":  "sticker-pack",
						"actionType":  "CLAIM_BENEFIT",
						"claimStatus": "CLAIMABLE",
						"benefit":     map[string]any{"kind": "STICKER", "amount": 1.0},
					},
					// A non-CLAIM_BENEFIT entry must be ignored.
					{
						"campaignId":  "streak-info",
						"actionType":  "VIEW",
						"claimStatus": "CLAIMABLE",
					},
				},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/claim"):
			claimHits.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"status": "CLAIMED"},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer openapi.Close()
	CheckinEndpointHook = func(*qauth.RegionConfig) *checkinRegion {
		return &checkinRegion{base: openapi.URL, origin: "https://stub.test"}
	}
	checkinHTTPClient = openapi.Client()

	b := newTestBridge(t, "fake-pat-secret-0123456789abcdef")

	result, err := b.Checkin(context.Background())
	if err != nil {
		t.Fatalf("Checkin err = %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("status = %q want success", result.Status)
	}
	if result.RewardCredits != 50 {
		t.Fatalf("reward = %g want 50", result.RewardCredits)
	}
	if !strings.Contains(result.Message, "50") {
		t.Fatalf("message = %q should mention 50 credits", result.Message)
	}
	if campaignsHits.Load() != 1 {
		t.Fatalf("campaigns GET hits = %d want 1", campaignsHits.Load())
	}
	// checkin.mjs claims EVERY CLAIMABLE CLAIM_BENEFIT campaign, not just
	// CREDITS-benefit ones — the benefit kind only affects the reward sum.
	// The VIEW entry is skipped (not CLAIM_BENEFIT). So 2 claims fire here:
	// the CREDITS daily + the STICKER pack.
	if claimHits.Load() != 2 {
		t.Fatalf("claim POST hits = %d want 2 (all CLAIMABLE CLAIM_BENEFIT)", claimHits.Load())
	}

	// Header parity with worker/src/checkin.mjs.
	if got := gotHeaders.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
		t.Fatalf("Authorization = %q want Bearer <token>", got)
	}
	if got := gotHeaders.Get("Cosy-ClientType"); got != "10" {
		t.Fatalf("Cosy-ClientType = %q want 10", got)
	}
	if got := gotHeaders.Get("Cosy-Version"); got != "0.3.4" {
		t.Fatalf("Cosy-Version = %q want 0.3.4", got)
	}
	for _, key := range []string{"Cosy-Machineid", "Cosy-Machinetoken", "Cosy-Machinetype", "Cosy-Machinecode"} {
		if gotHeaders.Get(key) == "" {
			t.Fatalf("%s header must be non-empty (a partial identity drops the credit campaign)", key)
		}
	}
	if got := gotHeaders.Get("Origin"); got != "https://stub.test" {
		t.Fatalf("Origin = %q want https://stub.test", got)
	}
	if got := gotHeaders.Get("Referer"); !strings.HasPrefix(got, openapi.URL+"/growth-page/activity-iframe") {
		t.Fatalf("Referer = %q want growth-page/activity-iframe", got)
	}
}

// TestBridgeCheckinAlreadyClaimed verifies the CLAIMED → "already" path.
func TestBridgeCheckinAlreadyClaimed(t *testing.T) {
	origHook := CheckinEndpointHook
	origClient := checkinHTTPClient
	t.Cleanup(func() {
		CheckinEndpointHook = origHook
		checkinHTTPClient = origClient
	})
	openapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"campaigns": []map[string]any{
				{"campaignId": "c1", "actionType": "CLAIM_BENEFIT", "claimStatus": "CLAIMED",
					"benefit": map[string]any{"kind": "CREDITS", "amount": 50.0}},
				{"campaignId": "c2", "actionType": "CLAIM_BENEFIT", "claimStatus": "CLAIMED"},
				// One CLAIMABLE non-credit campaign alone must not trigger a claim.
				{"campaignId": "c3", "actionType": "CLAIM_BENEFIT", "claimStatus": "CLAIMABLE",
					"benefit": map[string]any{"kind": "STICKER"}},
				// And no CLAIMABLE CREDITS campaign exists either.
			},
		})
	}))
	// Actually the test data above has a CLAIMABLE sticker but no CLAIMABLE credits,
	// so the result should be "skipped" (no claimable credits), not "already".
	// Let me redo: want "already" when ALL credit campaigns are CLAIMED.
	_ = openapi
	// (This case is covered by the third sub-test below; keeping the structure.)
	t.Skip("consolidated into TestBridgeCheckinNoClaimable")
}

// TestBridgeCheckinNoClaimable covers three response shapes that all yield a
// non-success terminal state without a POST /claim:
//   - empty campaigns list → "skipped"
//   - all-CLAIMED credit campaigns → "already"
//   - 404 on the GET → "skipped" (activity not open today)
func TestBridgeCheckinNoClaimable(t *testing.T) {
	origHook := CheckinEndpointHook
	origClient := checkinHTTPClient
	t.Cleanup(func() {
		CheckinEndpointHook = origHook
		checkinHTTPClient = origClient
	})
	stubRiskIdentity(t, nil)

	cases := []struct {
		name       string
		resp       map[string]any
		status     int
		wantStatus string
		wantMsg    string
	}{
		{
			name:       "empty campaigns",
			resp:       map[string]any{"campaigns": []map[string]any{}},
			wantStatus: "skipped",
			wantMsg:    "签到活动未开放",
		},
		{
			name: "all claimed",
			resp: map[string]any{
				"campaigns": []map[string]any{
					{"campaignId": "c1", "actionType": "CLAIM_BENEFIT", "claimStatus": "CLAIMED",
						"benefit": map[string]any{"kind": "CREDITS", "amount": 50.0}},
					{"campaignId": "c2", "actionType": "CLAIM_BENEFIT", "claimStatus": "CLAIMED",
						"benefit": map[string]any{"kind": "CREDITS", "amount": 100.0}},
				},
			},
			wantStatus: "already",
			wantMsg:    "今日已签到",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			openapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				_ = json.NewEncoder(w).Encode(tc.resp)
			}))
			defer openapi.Close()
			CheckinEndpointHook = func(*qauth.RegionConfig) *checkinRegion {
				return &checkinRegion{base: openapi.URL, origin: "https://stub.test"}
			}
			checkinHTTPClient = openapi.Client()

			b := newTestBridge(t, "fake-pat-secret-0123456789abcdef")
			result, err := b.Checkin(context.Background())
			if err != nil {
				t.Fatalf("Checkin err = %v", err)
			}
			if result.Status != tc.wantStatus {
				t.Fatalf("status = %q want %q", result.Status, tc.wantStatus)
			}
			if result.Message != tc.wantMsg {
				t.Fatalf("message = %q want %q", result.Message, tc.wantMsg)
			}
		})
	}

	// 404 path: campaigns endpoint returns 404 → "skipped".
	t.Run("404 activity not open", func(t *testing.T) {
		openapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("not found"))
		}))
		defer openapi.Close()
		CheckinEndpointHook = func(*qauth.RegionConfig) *checkinRegion {
			return &checkinRegion{base: openapi.URL, origin: "https://stub.test"}
		}
		checkinHTTPClient = openapi.Client()
		b := newTestBridge(t, "fake-pat-secret-0123456789abcdef")
		result, err := b.Checkin(context.Background())
		if err != nil {
			t.Fatalf("Checkin err = %v (404 should map to skipped, not error)", err)
		}
		if result.Status != "skipped" {
			t.Fatalf("status = %q want skipped", result.Status)
		}
	})
}

// newTestBridge constructs a Bridge with a fake PAT and pre-populates the
// session state so EnsureFreshSession is a no-op (no real jobToken exchange).
// The PAT is real enough that machine identity derivation runs; the session
// is real enough that CurrentSecurityOauth returns a usable bearer.
func newTestBridge(t *testing.T, pat string) *Bridge {
	t.Helper()
	mid, mtok, mtyp := mustDeriveMachineIdentity(pat)
	b := NewBridge(pat, qauth.CN, mid, mtok, mtyp)
	// Pre-populate the session: applyJobToken builds a real signed session
	// from a fake jobToken response, with a far-future expiry so needsRefresh
	// is false and EnsureFreshSession skips the network call entirely.
	fakeJT := map[string]any{
		"id":                 "test-uid-12345",
		"name":               "tester",
		"securityOauthToken": "test-security-oauth-token",
		"refreshToken":       "test-refresh-token",
		"expireTime":         float64(time.Now().Add(24 * time.Hour).UnixMilli()),
	}
	b.applyJobToken(fakeJT, "personal_standard")
	b.bootstrapped.Store(true)
	return b
}

func mustDeriveMachineIdentity(pat string) (string, string, string) {
	id, tok, typ := deriveMachineIdentity(pat)
	if id == "" || tok == "" || typ == "" {
		panic("deriveMachineIdentity returned empty field")
	}
	return id, tok, typ
}

// TestBridgeCheckinUsesGenuineFingerprint verifies that a valid desktop
// fingerprint overrides Cosy-MachineToken/Type/Code while the machine id stays
// the session-derived value.
func TestBridgeCheckinUsesGenuineFingerprint(t *testing.T) {
	origHook := CheckinEndpointHook
	origClient := checkinHTTPClient
	t.Cleanup(func() {
		CheckinEndpointHook = origHook
		checkinHTTPClient = origClient
	})
	stubRiskIdentity(t, &RiskIdentity{MachineToken: "genuine-tok", MachineType: "genuine-type", MachineCode: "genuine-code"})

	var gotHeaders http.Header
	openapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"campaigns": []map[string]any{{
				"campaignId": "c1", "actionType": "CLAIM_BENEFIT", "claimStatus": "CLAIMED",
				"benefit": map[string]any{"kind": "CREDITS", "amount": 100.0},
			}},
		})
	}))
	defer openapi.Close()
	CheckinEndpointHook = func(*qauth.RegionConfig) *checkinRegion {
		return &checkinRegion{base: openapi.URL, origin: "https://stub.test"}
	}
	checkinHTTPClient = openapi.Client()

	b := newTestBridge(t, "fake-pat-secret-0123456789abcdef")
	if _, err := b.Checkin(context.Background()); err != nil {
		t.Fatalf("Checkin err = %v", err)
	}
	if got := gotHeaders.Get("Cosy-MachineToken"); got != "genuine-tok" {
		t.Errorf("Cosy-MachineToken = %q want genuine-tok", got)
	}
	if got := gotHeaders.Get("Cosy-MachineType"); got != "genuine-type" {
		t.Errorf("Cosy-MachineType = %q want genuine-type", got)
	}
	if got := gotHeaders.Get("Cosy-MachineCode"); got != "genuine-code" {
		t.Errorf("Cosy-MachineCode = %q want genuine-code", got)
	}
	if got := gotHeaders.Get("Cosy-MachineId"); got != b.MachineID() {
		t.Errorf("Cosy-MachineId = %q want session-derived %q", got, b.MachineID())
	}
}

// TestBridgeCheckinGlobalUsesOpenAPIBearer verifies that a region flagged
// NeedsOpenAPIToken authenticates the campaign call with the OpenAPI bearer
// minted from /api/v1/jobToken/exchange, not the COSY securityOauthToken.
func TestBridgeCheckinGlobalUsesOpenAPIBearer(t *testing.T) {
	origHook := CheckinEndpointHook
	origClient := checkinHTTPClient
	t.Cleanup(func() {
		CheckinEndpointHook = origHook
		checkinHTTPClient = origClient
	})
	stubRiskIdentity(t, nil)

	var campaignsAuth string
	openapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/jobToken/exchange":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "openapi-token-xyz", "refresh_token": "openapi-rt",
				"expires_at": "2099-01-01T00:00:00Z",
			})
		case r.URL.Path == "/sash/api/v1/me/campaigns":
			campaignsAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"campaigns": []map[string]any{{
					"campaignId": "c1", "actionType": "CLAIM_BENEFIT", "claimStatus": "CLAIMED",
					"benefit": map[string]any{"kind": "CREDITS", "amount": 100.0},
				}},
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer openapi.Close()
	CheckinEndpointHook = func(*qauth.RegionConfig) *checkinRegion {
		return &checkinRegion{base: openapi.URL, origin: "https://stub.test"}
	}
	checkinHTTPClient = openapi.Client()

	mid, mtok, mtyp := mustDeriveMachineIdentity("fake-pat-secret-0123456789abcdef")
	region := &qauth.RegionConfig{Name: "global", OpenAPIBase: openapi.URL, NeedsOpenAPIToken: true}
	b := NewBridge("fake-pat-secret-0123456789abcdef", region, mid, mtok, mtyp)
	fakeJT := map[string]any{
		"id": "test-uid-12345", "name": "tester",
		"securityOauthToken": "cosy-sot", "refreshToken": "rt",
		"expireTime": float64(time.Now().Add(24 * time.Hour).UnixMilli()),
	}
	b.applyJobToken(fakeJT, "personal_standard")
	b.bootstrapped.Store(true)

	if _, err := b.Checkin(context.Background()); err != nil {
		t.Fatalf("Checkin err = %v", err)
	}
	if campaignsAuth != "Bearer openapi-token-xyz" {
		t.Fatalf("campaign Authorization = %q want the OpenAPI bearer", campaignsAuth)
	}
}
