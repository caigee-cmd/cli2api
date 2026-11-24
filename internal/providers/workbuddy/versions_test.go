package workbuddy

import (
	"net/http"
	"testing"
)

func TestClientVersionsAndDesktopUserAgents(t *testing.T) {
	if CLIVersion != "2.176.0" || IDEVersion != "2.176.0" {
		t.Fatalf("CLI=%q IDE=%q", CLIVersion, IDEVersion)
	}
	if UserAgent != "WorkBuddy/5.11.3 WorkBuddy/5.11.3 CLI/2.176.0" {
		t.Fatalf("CN desktop UA=%q", UserAgent)
	}
	if CLIStainlessPackageVersion != "6.25.0" || CLIStainlessRuntimeVersion != "v22.21.1" {
		t.Fatalf("SDK fingerprint package=%q runtime=%q", CLIStainlessPackageVersion, CLIStainlessRuntimeVersion)
	}
	if DesktopVersionGlobal != "5.11.4" || DesktopCLIVersionGlobal != "2.176.0" {
		t.Fatalf("global desktop=%q cli=%q", DesktopVersionGlobal, DesktopCLIVersionGlobal)
	}
	if got := DesktopUserAgentFor(true); got != "WorkBuddy/5.11.4 workbuddy-ai/5.11.4 CLI/2.176.0" {
		t.Fatalf("global desktop UA=%q", got)
	}
	if got := DesktopUserAgentFor(false); got != UserAgent {
		t.Fatalf("CN desktop UA=%q", got)
	}
	if got := DaemonUserAgentFor(true); got != "workbuddy-ai/5.11.4 workbuddy-ai/5.11.4 CLI/2.176.0" {
		t.Fatalf("global daemon UA=%q", got)
	}
	if got := DaemonUserAgentFor(false); got != UserAgent {
		t.Fatalf("CN daemon UA=%q", got)
	}
	if DesktopUserAgent != DesktopUserAgentGlobal {
		t.Fatalf("DesktopUserAgent must alias the global UA")
	}
	h := http.Header{}
	SetCatalogHeaders(h, Credential{AccessToken: "at", Domain: DomainGlobal})
	if h.Get("User-Agent") != DesktopUserAgentGlobal {
		t.Fatalf("global catalog UA=%q", h.Get("User-Agent"))
	}
	billing := http.Header{}
	SetBillingHeaders(billing, Credential{AccessToken: "at", UID: "u1", Domain: DomainCN})
	if billing.Get("User-Agent") != "axios/1.16.1" ||
		billing.Get("Accept") != "application/json" ||
		billing.Get("X-Client-Version") != DesktopVersionCN ||
		billing.Get("X-Client-Product") != "WorkBuddy" ||
		billing.Get("X-Request-ID") == "" {
		t.Fatalf("CN billing identity=%+v", billing)
	}
}
