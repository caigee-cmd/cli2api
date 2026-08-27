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
	candidates := make([]Release, 0, len(releases))
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		version, err := ParseVersion(release.TagName)
		if err != nil || version.Compare(currentVersion) <= 0 {
			continue
		}
		release.TagName = version.String()
		candidates = append(candidates, release)
	}
	if len(candidates) == 0 {
		return Release{}, false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, _ := ParseVersion(candidates[i].TagName)
		right, _ := ParseVersion(candidates[j].TagName)
		return left.Compare(right) < 0
	})
	return candidates[len(candidates)-1], true
}

// UpgradePath lists every stable release strictly between current and target,
// in ascending order, so the console can show what a direct update passes over.
// Versions the release list does not cover are silently skipped.
func UpgradePath(current, target string, releases []Release) []string {
	currentVersion, err := ParseVersion(current)
	if err != nil {
		return nil
	}
	targetVersion, err := ParseVersion(target)
	if err != nil || targetVersion.Compare(currentVersion) <= 0 {
		return nil
	}
	intermediate := make([]Release, 0, len(releases))
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		version, err := ParseVersion(release.TagName)
		if err != nil {
			continue
		}
		if version.Compare(currentVersion) > 0 && version.Compare(targetVersion) < 0 {
			release.TagName = version.String()
			intermediate = append(intermediate, release)
		}
	}
	if len(intermediate) == 0 {
		return nil
	}
	sort.SliceStable(intermediate, func(i, j int) bool {
		left, _ := ParseVersion(intermediate[i].TagName)
		right, _ := ParseVersion(intermediate[j].TagName)
		return left.Compare(right) < 0
	})
	path := make([]string, 0, len(intermediate))
	for _, release := range intermediate {
		path = append(path, release.TagName)
	}
	return path
}
