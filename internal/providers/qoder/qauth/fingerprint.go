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
// body version and the OpenAPI User-Agent all derive from it. It mirrors the
// pinned qodercli build (1.1.75; hub PR #75 / v1.7.3).
const CosyVersion = "1.1.75"

// DesktopCosyVersion is the Cosy-Version the official desktop app sends on
// campaign / check-in requests (hub desktop 0.4.8). Distinct from CosyVersion
// (CLI / reasoning).
const DesktopCosyVersion = "0.4.8"

// CosyUserAgent is the User-Agent official qodercli 1.1.75 leaves unset so the
// bundled undici fetch supplies its default ("undici").
const CosyUserAgent = "undici"

// CosyMachineOS mirrors qodercli's `${arch}_${platform}` value for a Linux
// x86_64 host, which is how cli2api is deployed.
const CosyMachineOS = "x86_64_linux"

// CosyClientIP is the link-local address official qodercli puts in
// cosy-clientip on outbound COSY requests.
const CosyClientIP = "169.254.198.161"

// applyCosyBusinessHeaders adds the business / scene / client-ip headers
// qodercli 1.1.75 sends on every COSY request.
func applyCosyBusinessHeaders(h http.Header) {
	h.Set("cosy-business-product", "cli")
	h.Set("cosy-business-type", "agent")
	h.Set("cosy-scene", "assistant")
	h.Set("cosy-clientip", CosyClientIP)
}

// UserAgent returns the COSY reasoning User-Agent. Official qodercli does not
// set UA itself; undici supplies "undici". Edition no longer splits the UA
// (hub PR #75). Region is accepted for call-site compatibility.
func UserAgent(region *RegionConfig) string {
	_ = region
	return CosyUserAgent
}

// UserAgentForURL returns the COSY User-Agent for a request URL.
func UserAgentForURL(rawURL string) string {
	_ = rawURL
	return CosyUserAgent
}

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

// applyClientFingerprint sets the undici User-Agent, always sends
// cosy-machineos (qodercli 1.1.75), and on inference SSE requests only also
// sends cosy-machinehostname.
func applyClientFingerprint(h http.Header, rawURL, uid string, inferSSE bool) {
	_ = rawURL
	h.Set("user-agent", CosyUserAgent)
	h.Set("cosy-machineos", CosyMachineOS)
	if inferSSE {
		h.Set("cosy-machinehostname", MachineHostname(uid))
	}
}
