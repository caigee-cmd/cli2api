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
	for _, tc := range []struct{ region, ua string }{{"global", "Qoder/"}, {"cn", "QoderWork/"}} {
		h, err := s.Sign("b", ChatEndpoint(tc.region), time.Unix(1, 0))
		if err != nil {
			t.Fatal(err)
		}
		if h["User-Agent"] != tc.ua+qauth.CosyVersion || h["Cosy-Version"] != qauth.CosyVersion {
			t.Fatalf("%s ua=%q ver=%q", tc.region, h["User-Agent"], h["Cosy-Version"])
		}
		if h["Cosy-MachineOS"] != qauth.CosyMachineOS || h["Cosy-MachineHostname"] != qauth.MachineHostname("uid-fp") {
			t.Fatalf("%s os/host=%q/%q", tc.region, h["Cosy-MachineOS"], h["Cosy-MachineHostname"])
		}
	}
}
