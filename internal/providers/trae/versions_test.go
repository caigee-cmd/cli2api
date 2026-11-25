package trae

import "testing"

// Trae serves its model catalog and chat by client build-date cohort: the
// X-Ide-Version-Code / X-App-Version-Code headers carry IdeVersionCode, and a
// value before the current cohort silently downgrades the account to a legacy
// catalog. Measured against the live CN upstream on 2026-11-25: 20260212
// returned 45 entries / 6 visible models and answered current models with
// 4001, while 20260811 and anything newer returned 65 entries / 21 visible
// models. The version string itself is not gated.
func TestIdeVersionCodeIsInCurrentModelCohort(t *testing.T) {
	if len(IdeVersionCode) != 8 {
		t.Fatalf("IdeVersionCode=%q must be an eight-digit build date", IdeVersionCode)
	}
	for _, digit := range IdeVersionCode {
		if digit < '0' || digit > '9' {
			t.Fatalf("IdeVersionCode=%q must be numeric", IdeVersionCode)
		}
	}
	const minModelCohort = "20261117" // v3.4.21 release build; bump with IdeVersionCode when the client line moves
	if IdeVersionCode < minModelCohort {
		t.Fatalf("IdeVersionCode=%q is below the %s model cohort; the catalog would collapse to the legacy six", IdeVersionCode, minModelCohort)
	}
}
