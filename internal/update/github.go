package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultGitHubAPI = "https://api.github.com"

type GitHubReleaseSource struct {
	client  *http.Client
	baseURL string
	repo    string
	token   string
}

func NewGitHubReleaseSource(repo, token string) *GitHubReleaseSource {
	client := &http.Client{Timeout: 20 * time.Second}
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if !isTrustedGitHubAPI(request.URL) {
			request.Header.Del("Authorization")
		}
		return nil
	}
	return &GitHubReleaseSource{client: client, baseURL: defaultGitHubAPI, repo: repo, token: strings.TrimSpace(token)}
}

func (s *GitHubReleaseSource) ListReleases(ctx context.Context) ([]Release, error) {
	if strings.TrimSpace(s.repo) == "" {
		return nil, fmt.Errorf("update repository required")
	}
	releases := make([]Release, 0, 100)
	for page := 1; ; page++ {
		batch, err := s.listReleasePage(ctx, page)
		if err != nil {
			return nil, err
		}
		releases = append(releases, batch...)
		if len(batch) < 100 {
			return releases, nil
		}
	}
}

func (s *GitHubReleaseSource) listReleasePage(ctx context.Context, page int) ([]Release, error) {
	requestURL := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", strings.TrimRight(s.baseURL, "/"), s.repo, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "cli2api-updater")
	if s.token != "" && isTrustedGitHubAPI(req.URL) {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch releases: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub releases returned %d", resp.StatusCode)
	}
	var releases []Release
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	if err := decoder.Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode releases: %w", err)
	}
	return releases, nil
}

func isTrustedGitHubAPI(value *url.URL) bool {
	return value != nil && value.Scheme == "https" && value.User == nil && strings.EqualFold(value.Host, "api.github.com")
}
