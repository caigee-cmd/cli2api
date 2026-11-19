package kiro

import "sync"

// Host builders stay behind these functions so tests can point every Kiro
// call at a local server without changing production URLs.
var (
	hostMu       sync.RWMutex
	chatOverride string
	restOverride string
)

func chatBaseFor(cred Credential) string {
	hostMu.RLock()
	defer hostMu.RUnlock()
	if chatOverride != "" {
		return chatOverride
	}
	return "https://" + runtimeHost(cred.dataPlaneRegion())
}

func chatBase() string { return chatBaseFor(Credential{}) }

func restBase(cred Credential) string {
	hostMu.RLock()
	override := restOverride
	hostMu.RUnlock()
	if override != "" {
		return override
	}
	return trimRightSlash(regionalizeURL(modelAPIBase, cred.dataPlaneRegion()))
}

func codeWhispererBase(cred Credential) string {
	hostMu.RLock()
	override := restOverride
	hostMu.RUnlock()
	if override != "" {
		return override
	}
	return trimRightSlash(regionalizeURL(restAPIBase, cred.dataPlaneRegion()))
}

func modelBase(cred Credential) string { return restBase(cred) }

func profileBase(region string) string {
	hostMu.RLock()
	override := restOverride
	hostMu.RUnlock()
	if override != "" {
		return override
	}
	return trimRightSlash(regionalizeURL(restAPIBase, normalizeRegion(region)))
}

func trimRightSlash(value string) string {
	for len(value) > 0 && value[len(value)-1] == '/' {
		value = value[:len(value)-1]
	}
	return value
}

func setTestHosts(base string) func() {
	hostMu.Lock()
	prevChat, prevRest := chatOverride, restOverride
	chatOverride, restOverride = base, base
	hostMu.Unlock()
	return func() {
		hostMu.Lock()
		chatOverride, restOverride = prevChat, prevRest
		hostMu.Unlock()
	}
}
