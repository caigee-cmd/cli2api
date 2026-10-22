package qauth

import (
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
)

// CosyVersion is the single client/protocol version cli2api presents to
// Qoder: the cosy-version header, the bearer payload's cosyVersion, the chat
// body version and the User-Agent all derive from it. It mirrors the pinned
// qodercli build cli2api emulates (session_type "qodercli", product "cli").
const CosyVersion = "1.1.32"

// CosyMachineOS mirrors qodercli's `${arch}_${platform}` value for a Linux
// x86_64 host, which is how cli2api is deployed.
const CosyMachineOS = "x86_64_linux"

// User-Agent product names per edition. The China edition ships as
// QoderWork, the international edition as Qoder.
const (
	userAgentProductCN     = "QoderWork"
	userAgentProductGlobal = "Qoder"
)

// UserAgent returns the edition-specific User-Agent. Anything that is not the
// international region is treated as the China edition (the default region).
func UserAgent(region *RegionConfig) string {
	if region != nil && region.Name == Global.Name {
		return userAgentProductGlobal + "/" + CosyVersion
	}
	return userAgentProductCN + "/" + CosyVersion
}

// UserAgentForURL returns the edition User-Agent for a request URL.
func UserAgentForURL(rawURL string) string { return UserAgent(regionForURL(rawURL)) }

// regionForURL infers the edition from a request URL's host.
func regionForURL(rawURL string) *RegionConfig {
	if u, err := url.Parse(rawURL); err == nil {
		host := strings.ToLower(u.Hostname())
		if host == "qoder.sh" || strings.HasSuffix(host, ".qoder.sh") {
			return Global
		}
	}
	return CN
}

// MachineHostname derives a stable, header-safe hostname for an account so
// every request from one account looks like one machine, and different
// accounts look like different machines.
func MachineHostname(uid string) string {
	if uid == "" {
		uid = "anonymous"
	}
	sum := md5.Sum([]byte("hostname:" + uid))
	return "DESKTOP-" + strings.ToUpper(hex.EncodeToString(sum[:])[:7])
}

// applyClientFingerprint sets the edition User-Agent and, on inference SSE
// requests only (as qodercli does), cosy-machineos/cosy-machinehostname.
func applyClientFingerprint(h http.Header, rawURL, uid string, inferSSE bool) {
	h.Set("user-agent", UserAgentForURL(rawURL))
	if inferSSE {
		h.Set("cosy-machineos", CosyMachineOS)
		h.Set("cosy-machinehostname", MachineHostname(uid))
	}
}
