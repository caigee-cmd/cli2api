package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestGitHubReleaseSourcePaginatesAllReleases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "100" {
			t.Fatalf("per_page = %q", r.URL.Query().Get("per_page"))
		}
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Fatal(err)
		}
		releases := make([]Release, 0, 100)
		if page == 1 {
			for patch := 102; patch >= 3; patch-- {
				releases = append(releases, Release{TagName: "v0.2." + strconv.Itoa(patch)})
			}
		} else if page == 2 {
			releases = append(releases, Release{TagName: "v0.2.2"})
		}
		if err := json.NewEncoder(w).Encode(releases); err != nil {
			t.Fatal(err)
		}
	}))
	t.Cleanup(server.Close)

	source := NewGitHubReleaseSource("caigee-cmd/cli2api", "")
	source.baseURL = server.URL
	releases, err := source.ListReleases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 101 || releases[len(releases)-1].TagName != "v0.2.2" {
		t.Fatalf("releases = %d, last = %+v", len(releases), releases[len(releases)-1])
	}
	next, ok := SelectNextRelease("v0.2.1", releases)
	if !ok || next.TagName != "v0.2.2" {
		t.Fatalf("next = %+v, ok = %v", next, ok)
	}
}
