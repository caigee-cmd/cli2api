package workbuddy

import (
	"net/http"
	"testing"
)

func TestClientVersionsAndDesktopUserAgents(t *testing.T) {
	if CLIVersion != "2.161.0" || IDEVersion != "2.168.2" {
		t.Fatalf("CLI=%q IDE=%q", CLIVersion, IDEVersion)
	}
	if UserAgent != "CLI/2.161.0 CodeBuddy/2.161.0" {
		t.Fatalf("CLI UA=%q", UserAgent)
	}
	if got := DesktopUserAgentFor(true); got != "WorkBuddy/5.8.4 workbuddy-ai/5.8.4 CLI/2.161.0" {
		t.Fatalf("global desktop UA=%q", got)
	}
	if got := DesktopUserAgentFor(false); got != "WorkBuddy/5.8.3 WorkBuddy/5.8.3 CLI/2.161.0" {
		t.Fatalf("CN desktop UA=%q", got)
	}
	if DesktopUserAgent != DesktopUserAgentGlobal {
		t.Fatalf("DesktopUserAgent must alias the global UA")
	}
	h := http.Header{}
	SetCatalogHeaders(h, Credential{AccessToken: "at", Domain: DomainGlobal})
	if h.Get("User-Agent") != DesktopUserAgentGlobal {
		t.Fatalf("global catalog UA=%q", h.Get("User-Agent"))
	}
}
