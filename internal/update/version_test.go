package update

import "testing"

func TestSelectNextReleaseUsesImmediateStableVersion(t *testing.T) {
	releases := []Release{
		{TagName: "v0.2.4"},
		{TagName: "v0.2.2"},
		{TagName: "v0.2.3", Prerelease: true},
		{TagName: "v0.2.1"},
		{TagName: "v0.3.0", Draft: true},
	}

	next, ok := SelectNextRelease("v0.2.1", releases)
	if !ok {
		t.Fatal("expected a next release")
	}
	if next.TagName != "v0.2.2" {
		t.Fatalf("next release = %q, want v0.2.2", next.TagName)
	}
}

func TestSelectNextReleaseRejectsDevelopmentVersion(t *testing.T) {
	if _, ok := SelectNextRelease("dev", []Release{{TagName: "v0.2.2"}}); ok {
		t.Fatal("development builds must not be managed")
	}
}

func TestParseVersionRequiresStableSemver(t *testing.T) {
	for _, input := range []string{"", "latest", "v0.2", "v0.2.1-rc1", "0.2.1.4"} {
		if _, err := ParseVersion(input); err == nil {
			t.Fatalf("ParseVersion(%q) unexpectedly succeeded", input)
		}
	}
	for _, input := range []string{"v0.2.1", "0.2.1", "v10.20.30"} {
		if _, err := ParseVersion(input); err != nil {
			t.Fatalf("ParseVersion(%q): %v", input, err)
		}
	}
}
