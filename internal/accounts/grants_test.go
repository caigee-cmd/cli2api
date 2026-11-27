package accounts

import (
	"reflect"
	"testing"
)

func TestParseProviderGrant(t *testing.T) {
	valid := map[string]ProviderGrant{
		"qoder":            {Provider: "qoder", Region: ""},
		"workbuddy":        {Provider: "workbuddy", Region: ""},
		"workbuddy:cn":     {Provider: "workbuddy", Region: "cn"},
		"workbuddy:global": {Provider: "workbuddy", Region: "global"},
		"qoder:cn":         {Provider: "qoder", Region: "cn"},
		"qoder:global":     {Provider: "qoder", Region: "global"},
		"trae:cn":          {Provider: "trae", Region: "cn"},
		"WorkBuddy:CN":     {Provider: "workbuddy", Region: "cn"},
	}
	for input, want := range valid {
		got, err := ParseProviderGrant(input)
		if err != nil {
			t.Fatalf("ParseProviderGrant(%q) unexpected error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ParseProviderGrant(%q) = %+v, want %+v", input, got, want)
		}
		if got.String() != input && !(got.Provider != "" && got.String() == lowerASCII(input)) {
			t.Fatalf("round trip mismatch: %q -> %q", input, got.String())
		}
	}

	invalid := []string{
		"",
		"   ",
		":cn",
		"workbuddy:",
		"workbuddy:cn:extra",
		"workbuddy :cn",
		" workbuddy:cn",
		"workbuddy:cn ",
		"unknown",
		"unknown:cn",
		"workbuddy:unknown",
		"qoder:antarctica",
		"trae:global",
	}
	for _, input := range invalid {
		if _, err := ParseProviderGrant(input); err == nil {
			t.Fatalf("ParseProviderGrant(%q) must fail", input)
		}
	}
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}

func TestGrantsAllowed(t *testing.T) {
	cases := []struct {
		name     string
		allowed  []string
		provider string
		region   string
		want     bool
	}{
		{"empty allowlist admits everything", nil, "workbuddy", "global", true},
		{"empty allowlist admits cn", nil, "workbuddy", "cn", true},
		{"bare entry covers every region", []string{"workbuddy"}, "workbuddy", "cn", true},
		{"bare entry covers global", []string{"workbuddy"}, "workbuddy", "global", true},
		{"bare entry does not cover other family", []string{"workbuddy"}, "qoder", "cn", false},
		{"region entry matches same region", []string{"workbuddy:cn"}, "workbuddy", "cn", true},
		{"region entry rejects other region", []string{"workbuddy:cn"}, "workbuddy", "global", false},
		{"region entry rejects other family", []string{"workbuddy:cn"}, "qoder", "cn", false},
		{"two region entries cover both", []string{"workbuddy:cn", "workbuddy:global"}, "workbuddy", "global", true},
		{"two region entries cover cn too", []string{"workbuddy:cn", "workbuddy:global"}, "workbuddy", "cn", true},
		{"mixed families independently", []string{"workbuddy:cn", "qoder"}, "qoder", "cn", true},
		{"mixed families reject unlisted", []string{"workbuddy:cn", "qoder"}, "trae", "cn", false},
		{"malformed entry fails closed", []string{"workbuddy:cn", "garbage!!"}, "workbuddy", "global", false},
		{"malformed entry does not widen", []string{"garbage!!"}, "workbuddy", "cn", false},
		{"case insensitive grant", []string{"WorkBuddy:CN"}, "workbuddy", "cn", true},
		{"empty region account reads as global", []string{"workbuddy:global"}, "workbuddy", "", true},
		{"empty region account denied by cn-only", []string{"workbuddy:cn"}, "workbuddy", "", false},
		{"empty provider reads as qoder", []string{"qoder:cn"}, "", "cn", true},
	}
	for _, tc := range cases {
		if got := GrantsAllowed(tc.provider, tc.region, tc.allowed); got != tc.want {
			t.Fatalf("%s: GrantsAllowed(%q, %q, %v) = %v, want %v",
				tc.name, tc.provider, tc.region, tc.allowed, got, tc.want)
		}
	}
}

func TestProviderAllowedFamilyLevel(t *testing.T) {
	if !ProviderAllowed("workbuddy", []string{"workbuddy:cn"}) {
		t.Fatal("family check must pass when some region of the family is granted")
	}
	if ProviderAllowed("workbuddy", []string{"qoder:cn"}) {
		t.Fatal("family check must fail when the family is not granted")
	}
	if !ProviderAllowed("workbuddy", nil) {
		t.Fatal("empty allowlist admits every family")
	}
}

func TestGrantedRegions(t *testing.T) {
	regions, all := GrantedRegions("workbuddy", nil)
	if !all || regions != nil {
		t.Fatalf("empty allowlist must report all regions, got %v all=%v", regions, all)
	}
	regions, all = GrantedRegions("workbuddy", []string{"workbuddy"})
	if !all || regions != nil {
		t.Fatalf("bare entry must report all regions, got %v all=%v", regions, all)
	}
	regions, all = GrantedRegions("workbuddy", []string{"workbuddy:cn"})
	if all || !reflect.DeepEqual(regions, []string{"cn"}) {
		t.Fatalf("single region grant = %v all=%v, want [cn] all=false", regions, all)
	}
	regions, all = GrantedRegions("workbuddy", []string{"workbuddy:global", "workbuddy:cn", "workbuddy:cn"})
	if all || !reflect.DeepEqual(regions, []string{"global", "cn"}) {
		t.Fatalf("duplicate regions must dedup preserving order, got %v all=%v", regions, all)
	}
	regions, all = GrantedRegions("qoder", []string{"workbuddy:cn"})
	if all || len(regions) != 0 {
		t.Fatalf("unrelated family grants = %v all=%v, want empty not-all", regions, all)
	}
}

func TestNormalizeAPIKeyProvidersRegionGrants(t *testing.T) {
	got, err := NormalizeAPIKeyProviders([]string{"workbuddy:cn", "qoder"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"workbuddy:cn", "qoder"}) {
		t.Fatalf("region entries survive normalization: %v", got)
	}

	// Bare entry absorbs same-family region entries.
	got, err = NormalizeAPIKeyProviders([]string{"workbuddy", "workbuddy:cn", "workbuddy:global"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"workbuddy"}) {
		t.Fatalf("bare entry must absorb region entries: %v", got)
	}

	// Duplicates collapse.
	got, err = NormalizeAPIKeyProviders([]string{"workbuddy:cn", "WorkBuddy:CN", "workbuddy:cn"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"workbuddy:cn"}) {
		t.Fatalf("duplicates must collapse: %v", got)
	}

	// Empty and blank entries are skipped like before.
	got, err = NormalizeAPIKeyProviders([]string{"", "  ", "workbuddy:cn"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"workbuddy:cn"}) {
		t.Fatalf("blank entries must be skipped: %v", got)
	}

	// Strict rejections surface as errors.
	for _, bad := range [][]string{
		{"workbuddy:"},
		{":cn"},
		{"workbuddy:cn:x"},
		{"unknown:cn"},
		{"workbuddy:antarctica"},
	} {
		if _, err := NormalizeAPIKeyProviders(bad); err == nil {
			t.Fatalf("NormalizeAPIKeyProviders(%v) must fail", bad)
		}
	}
}

// TestNormalizeAPIKeyProvidersAllRegionsSubmission covers the frontend legacy
// expansion path: an untouched legacy key ("workbuddy") is edited and saved
// with every region of the family checked, which must round-trip as explicit
// region entries — never as a mixed bare+region list.
func TestNormalizeAPIKeyProvidersAllRegionsSubmission(t *testing.T) {
	got, err := NormalizeAPIKeyProviders([]string{"workbuddy:cn", "workbuddy:global"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"workbuddy:cn", "workbuddy:global"}) {
		t.Fatalf("all-regions submission must stay explicit: %v", got)
	}

	// Even if a caller somehow mixes bare and region entries, normalization
	// keeps only the bare one — the stored value can never widen beyond what
	// the bare entry already meant, and the routing gate below treats both
	// forms identically.
	got, err = NormalizeAPIKeyProviders([]string{"workbuddy", "workbuddy:cn"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"workbuddy"}) {
		t.Fatalf("mixed submission collapses to the bare entry: %v", got)
	}
	if !GrantsAllowed("workbuddy", "global", got) || !GrantsAllowed("workbuddy", "cn", got) {
		t.Fatal("bare entry must still cover both regions")
	}
}
