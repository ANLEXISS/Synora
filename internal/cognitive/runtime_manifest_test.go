package cognitive

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyBundleUsesImmutableManifestAndHashes(t *testing.T) {
	if _, err := os.Stat("/home/rock/synora-cognitive-mlp-v1/MANIFEST.json"); err != nil {
		t.Skip("external cognitive bundle is not mounted")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyBundle("/home/rock/synora-cognitive-mlp-v1", filepath.Join(root, "models", "cognitive", "MANIFEST.runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if verification.ManifestSHA256 == "" || len(verification.HeadNames()) != 4 {
		t.Fatalf("incomplete verification: %#v", verification)
	}
}

func TestSafetyGateRemainsAdvisoryAndReportsFilteredActions(t *testing.T) {
	catalog := ActionCatalog{SchemaVersion: ActionCatalogSchemaVersion, Actions: []ActionDescriptor{{ID: "notify.security@global", Slot: "notify.security@global", Capability: "notify.security", RiskClass: "high", Enabled: true}}}
	decision := ApplySafetyGate(CognitiveOutput{AdvisoryOnly: true, ProposedActionIDs: []string{"notify.security@global"}, FilteredActions: []FilteredAction{{ID: "camera.record@event_zone", Reason: "ledger_executed"}}}, catalog)
	if !decision.AdvisoryOnly || decision.ExecutorCalled || len(decision.AllowedActions) != 0 || len(decision.BlockedActions) != 2 {
		t.Fatalf("safety gate crossed or lost a reason: %#v", decision)
	}
}
