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
// body's business.version and the OpenAPI User-Agent all derive from it.
// 1.1.81 is the first qodercli that writes catalog-v7 and puts model_key on
// the business block the gateway attributes billing to. The published CLI is
// newer (1.1.89 as of 2026-11-13); this stays on the protocol floor rather
// than tracking every patch, and the desktop check-in version stays separate.
const CosyVersion = "1.1.81"

// DesktopCosyVersion is the Cosy-Version the official desktop app sends on
// campaign / check-in requests (hub desktop 0.4.8). Distinct from CosyVersion
// (CLI / reasoning).
const DesktopCosyVersion = "0.4.8"

// CosyUserAgent is the User-Agent official qodercli leaves unset so the
// bundled undici fetch supplies its default ("undici").
const CosyUserAgent = "undici"

// CosyMachineOS mirrors qodercli's `${arch}_${platform}` value for a Linux
// x86_64 host, which is how cli2api is deployed.
const CosyMachineOS = "x86_64_linux"

// CosyClientIP is the link-local address official qodercli puts in
// cosy-clientip on outbound COSY requests.
const CosyClientIP = "169.254.198.161"

// applyCosyBusinessHeaders adds the business / scene / client-ip headers
// qodercli sends on every COSY request.
func applyCosyBusinessHeaders(h http.Header) {
	h.Set("cosy-business-product", "cli")
	h.Set("cosy-business-type", "agent")
	h.Set("cosy-scene", "assistant")
	h.Set("cosy-clientip", CosyClientIP)
}

// UserAgent returns the COSY reasoning User-Agent. Official qodercli does not
// set UA itself; undici supplies "undici". Edition no longer splits the UA.
// Region is accepted for call-site compatibility.
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
// cosy-machineos, and on inference SSE requests only also sends
// cosy-machinehostname.
func applyClientFingerprint(h http.Header, rawURL, uid string, inferSSE bool) {
	_ = rawURL
	h.Set("user-agent", CosyUserAgent)
	h.Set("cosy-machineos", CosyMachineOS)
	if inferSSE {
		h.Set("cosy-machinehostname", MachineHostname(uid))
	}
}
