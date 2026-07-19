package demo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestInvestorCoreUsesRealEngineAndIsDeterministic(t *testing.T) {
	left, err := Run(context.Background(), Options{Scenario: "investor-core", Seed: 3501, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := Run(context.Background(), Options{Scenario: "investor-core", Seed: 3501, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if left.Snapshot.DurableDigest != right.Snapshot.DurableDigest {
		t.Fatalf("same seed changed durable digest: %s != %s", left.Snapshot.DurableDigest, right.Snapshot.DurableDigest)
	}
	if left.Snapshot.HypothesisCount == 0 {
		t.Fatal("real ambiguity fixture did not open a hypothesis")
	}
	if !left.Snapshot.ReplayEqual {
		t.Fatal("replay digest was not equal")
	}
	if left.Manifest.SecurityAuthority != "future" {
		t.Fatalf("security authority was overstated: %q", left.Manifest.SecurityAuthority)
	}
	encoded, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "Alice") || strings.Contains(string(encoded), "Rue ") || strings.Contains(string(encoded), "@") {
		t.Fatal("raw personal identifier leaked into synthetic export")
	}
}

func TestSeedChangesSyntheticTimeline(t *testing.T) {
	left, err := Run(context.Background(), Options{Scenario: "investor-core", Seed: 3501})
	if err != nil {
		t.Fatal(err)
	}
	right, err := Run(context.Background(), Options{Scenario: "investor-core", Seed: 3502})
	if err != nil {
		t.Fatal(err)
	}
	if left.Events[0].At.Equal(right.Events[0].At) {
		t.Fatal("different seed did not change a seeded synthetic event")
	}
}

func TestClaimsAreStableAndSecurityIsFuture(t *testing.T) {
	claims := Claims().Claims
	seen := map[string]bool{}
	for _, claim := range claims {
		if claim.ID == "" || seen[claim.ID] {
			t.Fatalf("invalid or duplicate claim %q", claim.ID)
		}
		seen[claim.ID] = true
	}
	for _, claim := range claims {
		if claim.ID == "security-authority" && claim.Status != "future" {
			t.Fatalf("security authority must remain future")
		}
	}
}
