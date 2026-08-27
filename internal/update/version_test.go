package update

import "testing"

func TestSelectNextReleaseTargetsLatestStableVersion(t *testing.T) {
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
	if next.TagName != "v0.2.4" {
		t.Fatalf("next release = %q, want v0.2.4", next.TagName)
	}
}

func TestSelectNextReleaseSkipsOlderVersions(t *testing.T) {
	releases := []Release{{TagName: "v0.1.9"}, {TagName: "v0.2.1"}, {TagName: "v0.2.2"}}

	if _, ok := SelectNextRelease("v0.2.2", releases); ok {
		t.Fatal("no release is newer than v0.2.2")
	}
}

func TestUpgradePathListsIntermediateStableVersions(t *testing.T) {
	releases := []Release{
		{TagName: "v0.2.3"},
		{TagName: "v0.2.2"},
		{TagName: "v0.2.4"},
		{TagName: "v0.2.3-rc1"},
		{TagName: "v0.1.9"},
	}

	path := UpgradePath("v0.2.1", "v0.2.4", releases)
	if len(path) != 2 || path[0] != "v0.2.2" || path[1] != "v0.2.3" {
		t.Fatalf("path = %v, want [v0.2.2 v0.2.3]", path)
	}
	if path := UpgradePath("v0.2.3", "v0.2.4", releases); path != nil {
		t.Fatalf("adjacent path = %v, want nil", path)
	}
	if path := UpgradePath("dev", "v0.2.4", releases); path != nil {
		t.Fatalf("development path = %v, want nil", path)
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
