package trae

import (
	"net/http"
	"testing"
)

// The UG checkin backend keys its per-device daily limit on X-Device-Id and
// answers a bare hex id with code 9074, so every stored id must go out in the
// aha-<hex> shape Trae's own client uses.
func TestSetUgHeadersNormalizesDeviceID(t *testing.T) {
	cases := []struct {
		name     string
		deviceID string
		want     string
	}{
		{"bare hex gets the aha prefix", "3ee0250fe3e0e6e03cdd8054e3a01a7b", "aha-3ee0250fe3e0e6e03cdd8054e3a01a7b"},
		{"prefixed id is left alone", "aha-3ee0250fe3e0e6e03cdd8054e3a01a7b", "aha-3ee0250fe3e0e6e03cdd8054e3a01a7b"},
		{"whitespace is trimmed", "  cfac6e546e2386ec29e13335393d79dc \n", "aha-cfac6e546e2386ec29e13335393d79dc"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			header := http.Header{}
			SetUgHeaders(header, Credential{AccessToken: "at", DeviceID: testCase.deviceID})
			if got := header.Get("X-Device-Id"); got != testCase.want {
				t.Fatalf("X-Device-Id=%q want %q", got, testCase.want)
			}
			if got := header.Get("Authorization"); got != "Cloud-IDE-JWT at" {
				t.Fatalf("Authorization=%q", got)
			}
		})
	}

	header := http.Header{}
	SetUgHeaders(header, Credential{AccessToken: "at"})
	if got := header.Get("X-Device-Id"); got != "" {
		t.Fatalf("missing device id must not be sent: %q", got)
	}
}
