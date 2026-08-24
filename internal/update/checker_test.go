package update

import (
	"context"
	"testing"
)

type releaseSourceStub struct {
	releases []Release
	err      error
	calls    int
}

func (s *releaseSourceStub) ListReleases(context.Context) ([]Release, error) {
	s.calls++
	return s.releases, s.err
}

func TestCheckerReturnsOnlyImmediateNextRelease(t *testing.T) {
	source := &releaseSourceStub{releases: []Release{
		{TagName: "v0.2.4", Name: "far"},
		{TagName: "v0.2.2", Name: "next"},
		{TagName: "v0.2.3", Name: "middle"},
	}}
	checker := NewChecker("v0.2.1", source)

	info, err := checker.Check(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Managed || !info.HasUpdate || info.NextVersion != "v0.2.2" {
		t.Fatalf("info = %+v", info)
	}
	if info.Release == nil || info.Release.Name != "next" {
		t.Fatalf("release = %+v", info.Release)
	}
}

func TestCheckerDisablesDevelopmentBuilds(t *testing.T) {
	source := &releaseSourceStub{releases: []Release{{TagName: "v0.2.2"}}}
	checker := NewChecker("dev", source)

	info, err := checker.Check(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if info.Managed || info.HasUpdate || source.calls != 0 {
		t.Fatalf("info=%+v calls=%d", info, source.calls)
	}
}

func TestCheckerForceCheckDoesNotUseStaleRelease(t *testing.T) {
	source := &releaseSourceStub{releases: []Release{{TagName: "v0.2.2"}}}
	checker := NewChecker("v0.2.1", source)
	if _, err := checker.Check(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	source.err = context.DeadlineExceeded
	if _, err := checker.Check(context.Background(), true); err == nil {
		t.Fatal("forced update check unexpectedly used stale cache")
	}
}
