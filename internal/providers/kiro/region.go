package kiro

import (
	"net/url"
	"regexp"
	"strings"
)

var (
	profileRegionPattern  = regexp.MustCompile(`^[a-z]{2}(?:-[a-z0-9]+)+-[0-9]+$`)
	profileAccountPattern = regexp.MustCompile(`^[0-9]{12}$`)
	profileIDPattern      = regexp.MustCompile(`^[A-Za-z0-9+=,.@_-]+$`)
)

// parseProfileArn accepts only a codewhisperer profile ARN. The region later
// becomes a hostname, so every part is checked before it is trusted.
func parseProfileArn(raw string) (canonical, region string, ok bool) {
	canonical = strings.TrimSpace(raw)
	parts := strings.SplitN(canonical, ":", 6)
	if len(parts) != 6 || parts[0] != "arn" || parts[1] != "aws" || parts[2] != "codewhisperer" ||
		!profileRegionPattern.MatchString(parts[3]) || !profileAccountPattern.MatchString(parts[4]) ||
		!strings.HasPrefix(parts[5], "profile/") {
		return "", "", false
	}
	if !profileIDPattern.MatchString(strings.TrimPrefix(parts[5], "profile/")) {
		return "", "", false
	}
	return canonical, parts[3], true
}

func regionFromProfile(profileArn string) string {
	_, region, ok := parseProfileArn(profileArn)
	if !ok {
		return ""
	}
	return region
}

// regionalizeURL rewrites a us-east-1 Amazon Q or CodeWhisperer URL onto the
// account's data plane. us-east-1 keeps the original host.
func regionalizeURL(rawURL, region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	if region == DefaultRegion || !profileRegionPattern.MatchString(region) {
		return rawURL
	}
	host := "q." + region + ".amazonaws.com"
	return strings.NewReplacer(
		"q.us-east-1.amazonaws.com", host,
		"codewhisperer.us-east-1.amazonaws.com", host,
	).Replace(rawURL)
}

func runtimeHost(region string) string {
	return "runtime." + normalizeRegion(region) + ".kiro.dev"
}

func withProfileQuery(rawURL, profileArn string) string {
	if profileArn == "" {
		return rawURL
	}
	return rawURL + "&profileArn=" + url.QueryEscape(profileArn)
}
