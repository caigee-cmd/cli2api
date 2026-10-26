package kimi

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ImportFromLocal is the import_from value that loads the machine's existing
// Kimi Code CLI login instead of tokens carried in the payload.
const ImportFromLocal = "local"

// ErrNoLocalCredential reports that no Kimi Code client credential was found on
// this machine.
var ErrNoLocalCredential = errors.New("no local Kimi Code credential found")

// kimiCodeHomeEnv is the environment variable the Kimi Code clients use to
// relocate their data directory.
const kimiCodeHomeEnv = "KIMI_CODE_HOME"

// LocalCredentialPaths lists the credential files the Kimi Code clients write,
// most specific first: KIMI_CODE_HOME, then the default ~/.kimi-code home, then
// the legacy kimi-cli ~/.kimi home.
func LocalCredentialPaths() []string {
	var paths []string
	add := func(home string) {
		home = strings.TrimSpace(home)
		if home == "" {
			return
		}
		candidate := filepath.Join(home, "credentials", "kimi-code.json")
		for _, existing := range paths {
			if existing == candidate {
				return
			}
		}
		paths = append(paths, candidate)
	}
	add(os.Getenv(kimiCodeHomeEnv))
	add(userHomeDir(".kimi-code"))
	add(userHomeDir(".kimi"))
	return paths
}

// LoadLocalCredential reads the first usable local Kimi Code credential and
// stamps the install's device id onto it. This is how an account reuses the
// login the user already completed with the official client.
func LoadLocalCredential() (Credential, error) {
	var lastErr error
	for _, path := range LocalCredentialPaths() {
		payload, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				lastErr = err
			}
			continue
		}
		credential, err := DecodeCredential(payload)
		if err != nil {
			lastErr = err
			continue
		}
		if !credential.Ready() {
			lastErr = errors.New(path + " has no access token")
			continue
		}
		if credential.DeviceID == "" {
			credential.DeviceID = readDeviceID(filepath.Dir(filepath.Dir(path)))
		}
		if credential.DeviceID == "" {
			credential.DeviceID = newDeviceID()
		}
		return credential, nil
	}
	if lastErr == nil {
		lastErr = ErrNoLocalCredential
	}
	return Credential{}, lastErr
}

// readDeviceID reads the device_id file the Kimi Code clients keep next to the
// credentials directory, so an imported login keeps reporting the same device.
func readDeviceID(home string) string {
	if strings.TrimSpace(home) == "" {
		return ""
	}
	payload, err := os.ReadFile(filepath.Join(home, "device_id"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(payload))
}

func userHomeDir(name string) string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	return filepath.Join(home, name)
}
