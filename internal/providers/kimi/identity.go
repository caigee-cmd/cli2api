package kimi

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// newDeviceID mints a random UUIDv4 for X-Msh-Device-Id. Kimi's clients mint one
// per install and keep it forever, so the value is persisted in the credential
// and reused for every request of that account.
func newDeviceID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand failure is fatal for identity: fall back to a stable
		// per-process value rather than sending an empty header.
		return fmt.Sprintf("00000000-0000-4000-8000-%012x", os.Getpid())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

// applyIdentityHeaders stamps the Kimi client identity onto an outgoing
// request. The Kimi Code clients send the product user agent plus this X-Msh-*
// block on chat, model and OAuth calls; /me and /usages are called with only
// Authorization and Accept, so callers there skip this helper.
func applyIdentityHeaders(req *http.Request, deviceID string) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("X-Msh-Platform", Platform)
	req.Header.Set("X-Msh-Version", ClientVersion)
	req.Header.Set("X-Msh-Device-Name", deviceName())
	req.Header.Set("X-Msh-Device-Model", deviceModel())
	req.Header.Set("X-Msh-Os-Version", osVersion())
	if id := strings.TrimSpace(deviceID); id != "" {
		req.Header.Set("X-Msh-Device-Id", id)
	}
}

func deviceName() string {
	name := strings.TrimSpace(os.Getenv("COMPUTERNAME"))
	if name == "" {
		if host, err := os.Hostname(); err == nil {
			name = strings.TrimSpace(host)
		}
	}
	if name == "" {
		return "unknown"
	}
	return asciiHeader(name)
}

// deviceModel mirrors the "Windows 11 amd64" shape Kimi's clients report.
func deviceModel() string {
	return asciiHeader(displayOS() + " " + runtime.GOARCH)
}

func osVersion() string {
	return asciiHeader(displayOS())
}

func displayOS() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows"
	case "darwin":
		return "macOS"
	default:
		return runtime.GOOS
	}
}

// asciiHeader strips non-printable and non-ASCII bytes: header values must stay
// a single printable ASCII token.
func asciiHeader(value string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return -1
		}
		return r
	}, value)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}
