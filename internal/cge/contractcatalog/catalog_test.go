package contractcatalog

import (
	"path/filepath"
	"runtime"
	"testing"
)

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

func TestCatalogIsValid(t *testing.T) {
	set, err := Validate(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Catalog.Contracts) == 0 || len(set.Boundaries.Boundaries) != 18 || len(set.Stores.Stores) == 0 {
		t.Fatalf("catalog unexpectedly incomplete: contracts=%d boundaries=%d stores=%d", len(set.Catalog.Contracts), len(set.Boundaries.Boundaries), len(set.Stores.Stores))
	}
}

func TestCatalogAuthorityAndDurablePrivacyGates(t *testing.T) {
	set, err := Validate(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if set.Catalog.Catalog.AuthorityCeiling != "advisory" {
		t.Fatalf("authority ceiling=%q", set.Catalog.Catalog.AuthorityCeiling)
	}
	for _, contract := range set.Catalog.Contracts {
		if len(contract.Persistence) == 0 && contract.ID != "synora.contract.action-request.v1" {
			continue
		}
		if contract.Owner != "automation" && contract.Owner != "historical_core" && (contract.Authority == "authorized_action" || contract.Authority == "authorized_decision") {
			t.Fatalf("CGE or non-historical contract has execution authority: %s=%s", contract.ID, contract.Authority)
		}
	}
	for _, store := range set.Stores.Stores {
		if store.Durable && (store.ClearSecretAllowed || store.ClearBiometricAllowed) {
			t.Fatalf("durable store permits clear sensitive data: %s", store.ID)
		}
	}
}

func TestAllowlistedEventsAndHistoricalMotion(t *testing.T) {
	set, err := Validate(repositoryRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"vision.identity": true, "vision.unknown": true, "vision.uncertain": true}
	for _, event := range set.Catalog.AdmissionEvents {
		if want[event.EventType] && (!event.Workflow || event.Disposition != "admitted") {
			t.Fatalf("allowlisted event not admitted: %+v", event)
		}
		if event.EventType == "vision.motion" && (event.Workflow || event.Disposition != "historical_only") {
			t.Fatalf("motion is not historical-only: %+v", event)
		}
	}
}
