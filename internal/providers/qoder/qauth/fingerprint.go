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
// 1.1.81 introduced catalog-v7 and model_key on the business block; the
// current official CLI is 1.1.96, whose COSY signing algorithm, headers and
// business endpoints are unchanged from 1.1.89. Keep this aligned with the
// published CLI rather than the older protocol floor. The desktop check-in
// version stays separate because campaign endpoints use their own identity.
const CosyVersion = "1.1.96"

// DesktopCosyVersionCN / DesktopCosyVersionGlobal are the Cosy-Version values
// the official desktop apps send on campaign / check-in requests, per edition.
// They track the published desktop releases rather than the CLI: Qoder CN is
// 0.5.18 (2026-11-24, changelog.qoder.cn/app) and the international app is
// 0.5.3 (2026-11-03, qoder.com/zh/changelog). Distinct from CosyVersion (CLI /
// reasoning). Campaign endpoints only care about the client type and identity;
// the value is attribution, not a gate, so keeping it on the current app
// release is safe.
const (
	DesktopCosyVersionCN     = "0.5.18"
	DesktopCosyVersionGlobal = "0.5.3"
)

// DesktopCosyVersion returns the desktop check-in version for an edition.
func DesktopCosyVersion(region *RegionConfig) string {
	if region != nil && region.Name == "global" {
		return DesktopCosyVersionGlobal
	}
	return DesktopCosyVersionCN
}

// CosyUserAgent is the User-Agent the current official qodercli leaves on
// COSY requests. The CLI runs on Bun and lets its bundled fetch supply
// "Bun/1.4.4"; older npm/Node builds sent "undici". Keep the edition split
// out of this: both regions use the same runtime.
const CosyUserAgent = "Bun/1.4.4"

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
// set UA itself; its Bun fetch supplies "Bun/1.4.4". Edition no longer splits
// the UA. Region is accepted for call-site compatibility.
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

// applyClientFingerprint sets the Bun User-Agent, always sends
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
