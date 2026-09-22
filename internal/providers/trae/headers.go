package trae

import (
	"net/http"
	"strings"
)

func clientUA() string { return UserAgent }

func SetOAuthHeaders(header http.Header) {
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set("User-Agent", clientUA())
}

func SetUgHeaders(header http.Header, credential Credential) {
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set("User-Agent", clientUA())
	if credential.AccessToken != "" {
		header.Set("Authorization", "Cloud-IDE-JWT "+credential.AccessToken)
	}
	header.Set("X-User-Region", "CN")
	if deviceID := ugDeviceID(credential.DeviceID); deviceID != "" {
		header.Set("X-Device-Id", deviceID)
	}
}

// ugDeviceID normalises the stored device id for the UG endpoints. The checkin
// backend keys its per-device daily limit on X-Device-Id and is picky about the
// shape: a bare hex id is rejected with 9074, so hex ids get the aha- prefix the
// IDE client uses; but a *numeric* id (the shape current logins issue) must be
// sent bare — "aha-<digits>" is itself rejected with 9074.
func ugDeviceID(deviceID string) string {
	trimmed := strings.TrimSpace(deviceID)
	if trimmed == "" || strings.HasPrefix(trimmed, "aha-") {
		return trimmed
	}
	if isAllDigits(trimmed) {
		return trimmed
	}
	return "aha-" + trimmed
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func SetSOLOHeaders(header http.Header, credential Credential, stream bool) {
	header.Set("Content-Type", "application/json")
	if stream {
		header.Set("Accept", "text/event-stream")
	} else {
		header.Set("Accept", "application/json")
	}
	header.Set("User-Agent", clientUA())
	if credential.AccessToken != "" {
		header.Set("Authorization", "Cloud-IDE-JWT "+credential.AccessToken)
		header.Set("X-Cloudide-Token", credential.AccessToken)
		header.Set("X-Ide-Token", credential.AccessToken)
	}
	if credential.UID != "" {
		header.Set("X-Uid", credential.UID)
	}
	header.Set("X-App-Id", AppID)
	header.Set("X-App-Version", "default")
	header.Set("X-Ide-Version", IdeVersion)
	header.Set("X-Ide-Version-Code", IdeVersionCode)
	header.Set("X-App-Version-Code", IdeVersionCode)
	header.Set("X-Ide-Version-Type", "stable")
	header.Set("X-Device-Type", "windows")
	header.Set("X-OS-Version", OSVersion)
	header.Set("X-Device-Brand", DeviceBrand)
	header.Set("Request-Traffic-Type", "prod")
	if credential.MachineID != "" {
		header.Set("X-Machine-Id", credential.MachineID)
	}
	if credential.DeviceID != "" {
		header.Set("X-Device-Id", credential.DeviceID)
	}
}

func SetChatHeaders(header http.Header, credential Credential) {
	SetSOLOHeaders(header, credential, true)
}

func SetCatalogHeaders(header http.Header, credential Credential) {
	SetSOLOHeaders(header, credential, false)
}
