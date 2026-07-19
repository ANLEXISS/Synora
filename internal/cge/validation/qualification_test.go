package validation

import (
	"context"
	"path/filepath"
	"testing"

	"synora/internal/cge/chains/association"
)

func TestCapabilityAudit(t *testing.T) {
	before, err := InspectAssociationCapabilities(association.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	after, err := InspectAssociationCapabilities(association.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if !before.AmbiguousAttachReachable || before.AmbiguousCreateReachable || before.ReasonCode != "association_create_candidate_not_reachable" || !before.TransactionalCreateCandidate {
		t.Fatalf("unexpected capability report: %+v", before)
	}
	if before.AttachExistingStatus != CapabilityReachable || before.CreateCandidateStatus != CapabilityDormant || before.AmbiguousCreateReachable != after.AmbiguousCreateReachable || before.Details == nil {
		t.Fatalf("capability audit is not deterministic: before=%+v after=%+v", before, after)
	}
}

func TestWALFailureMatrix(t *testing.T) {
	ok, total := runWALFailureMatrix(context.Background(), filepath.Join(t.TempDir(), "wal"))
	if !ok {
		t.Fatalf("WAL failure matrix failed: %d tests", total)
	}
}

func TestConcurrencyMatrix(t *testing.T) {
	ok, total := runConcurrencyMatrix(context.Background(), filepath.Join(t.TempDir(), "concurrency"))
	if !ok {
		t.Fatalf("concurrency matrix failed: %d tests", total)
	}
}

func TestCollisionAndIdempotenceMatrices(t *testing.T) {
	root := t.TempDir()
	if ok, total := runCollisionMatrix(context.Background(), filepath.Join(root, "collisions")); !ok || total != 4 {
		t.Fatalf("collision matrix failed: %d tests", total)
	}
	if ok, total := runIdempotenceMatrix(context.Background(), filepath.Join(root, "idempotence")); !ok || total != 5 {
		t.Fatalf("idempotence matrix failed: %d tests", total)
	}
}
