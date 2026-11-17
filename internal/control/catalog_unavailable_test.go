package control

import (
	"testing"

	"github.com/caigee-cmd/cli2api/internal/auth"
)

func TestAccountServesModelHonorsUnavailableModels(t *testing.T) {
	entry := map[string]any{"id": "trae/locked-model", "provider": "trae"}
	identity := auth.Identity{}

	if !ModelServedByAccounts(entry, identity, []AccountModelAllow{{
		Provider: "trae",
		Region:   "cn",
	}}) {
		t.Fatal("unrestricted healthy account must serve the model")
	}
	if ModelServedByAccounts(entry, identity, []AccountModelAllow{{
		Provider:          "trae",
		Region:            "cn",
		UnavailableModels: []string{"locked-model"},
	}}) {
		t.Fatal("plan-locked model must not be advertised by an account that cannot serve it")
	}
}
