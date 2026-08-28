package trae

import "net/http"

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
	if credential.DeviceID != "" {
		header.Set("X-Device-Id", credential.DeviceID)
	}
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
