package update

import (
	"context"
	"sync"
	"time"
)

type ReleaseSource interface {
	ListReleases(context.Context) ([]Release, error)
}

type Info struct {
	CurrentVersion string   `json:"current_version"`
	NextVersion    string   `json:"next_version,omitempty"`
	HasUpdate      bool     `json:"has_update"`
	Managed        bool     `json:"managed"`
	Cached         bool     `json:"cached"`
	Warning        string   `json:"warning,omitempty"`
	Release        *Release `json:"release,omitempty"`
}

type Checker struct {
	currentVersion string
	source         ReleaseSource
	cacheTTL       time.Duration
	mu             sync.Mutex
	cached         *Info
	expiresAt      time.Time
}

func NewChecker(currentVersion string, source ReleaseSource) *Checker {
	return &Checker{currentVersion: currentVersion, source: source, cacheTTL: 10 * time.Minute}
}

func (c *Checker) Check(ctx context.Context, force bool) (Info, error) {
	current, err := ParseVersion(c.currentVersion)
	if err != nil {
		return Info{CurrentVersion: c.currentVersion, Managed: false, Warning: "development build"}, nil
	}
	currentVersion := current.String()
	if !force {
		c.mu.Lock()
		if c.cached != nil && time.Now().Before(c.expiresAt) {
			info := *c.cached
			info.Cached = true
			c.mu.Unlock()
			return info, nil
		}
		c.mu.Unlock()
	}
	releases, err := c.source.ListReleases(ctx)
	if err != nil {
		if force {
			return Info{}, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.cached != nil {
			info := *c.cached
			info.Cached = true
			info.Warning = err.Error()
			return info, nil
		}
		return Info{}, err
	}
	info := Info{CurrentVersion: currentVersion, Managed: true}
	if next, ok := SelectNextRelease(currentVersion, releases); ok {
		info.HasUpdate = true
		info.NextVersion = next.TagName
		info.Release = &next
	}
	c.mu.Lock()
	c.cached = &info
	c.expiresAt = time.Now().Add(c.cacheTTL)
	c.mu.Unlock()
	return info, nil
}
