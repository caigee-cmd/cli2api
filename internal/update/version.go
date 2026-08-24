package update

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type Version struct {
	Major int
	Minor int
	Patch int
}

func ParseVersion(value string) (Version, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version must be x.y.z")
	}
	values := [3]int{}
	for index, part := range parts {
		if part == "" {
			return Version{}, fmt.Errorf("version must be x.y.z")
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 || strconv.Itoa(number) != part {
			return Version{}, fmt.Errorf("invalid stable version %q", value)
		}
		values[index] = number
	}
	return Version{Major: values[0], Minor: values[1], Patch: values[2]}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func (v Version) Compare(other Version) int {
	left := [3]int{v.Major, v.Minor, v.Patch}
	right := [3]int{other.Major, other.Minor, other.Patch}
	for index := range left {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	return 0
}

type Release struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

func SelectNextRelease(current string, releases []Release) (Release, bool) {
	currentVersion, err := ParseVersion(current)
	if err != nil {
		return Release{}, false
	}
	type candidate struct {
		release Release
		version Version
	}
	candidates := make([]candidate, 0, len(releases))
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		version, err := ParseVersion(release.TagName)
		if err != nil || version.Compare(currentVersion) <= 0 {
			continue
		}
		candidates = append(candidates, candidate{release: release, version: version})
	}
	if len(candidates) == 0 {
		return Release{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].version.Compare(candidates[j].version) < 0
	})
	next := candidates[0].release
	next.TagName = candidates[0].version.String()
	return next, true
}
