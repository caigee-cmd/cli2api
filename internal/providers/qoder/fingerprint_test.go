package qoder

import (
	"testing"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers/qoder/qauth"
)

func TestDirectSignCarriesClientFingerprint(t *testing.T) {
	if COSYVersion != qauth.CosyVersion {
		t.Fatal("COSYVersion must be the shared qauth constant")
	}
	s, err := NewSession(CLIUser{UID: "uid-fp", AccessToken: "dt"})
	if err != nil {
		t.Fatal(err)
	}
	for _, region := range []string{"global", "cn"} {
		h, err := s.Sign("b", ChatEndpoint(region), time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		if h["User-Agent"] != qauth.CosyUserAgent || h["Cosy-Version"] != qauth.CosyVersion {
			t.Fatalf("%s ua=%q ver=%q", region, h["User-Agent"], h["Cosy-Version"])
		}
		if h["Cosy-Data-Policy"] != "agree" {
			t.Fatalf("%s data-policy=%q", region, h["Cosy-Data-Policy"])
		}
		if h["Cosy-Business-Product"] != "cli" || h["Cosy-Business-Type"] != "agent" ||
			h["Cosy-Scene"] != "assistant" || h["Cosy-Clientip"] != qauth.CosyClientIP {
			t.Fatalf("%s business headers incomplete: %+v", region, h)
		}
		if h["Cosy-MachineOS"] != qauth.CosyMachineOS || h["Cosy-MachineHostname"] != qauth.MachineHostname("uid-fp") {
			t.Fatalf("%s os/host=%q/%q", region, h["Cosy-MachineOS"], h["Cosy-MachineHostname"])
		}
	}
}
