package contract

import (
	"testing"
	"time"
)

func TestPublicSnapshotIncludesHomeProjectionWithoutReplacingV1Fields(t *testing.T) {
	snapshot := PublicSnapshotFromCoreState(map[string]any{
		"revision": 7,
		"home":     HomeModelSnapshot{Version: "home-world-model-v1", GeneratedAt: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), Entities: []HomeEntity{{ID: "P17", Class: "person", Location: "garden", Confidence: .89, Status: "active"}}},
	})
	if snapshot.Revision != 7 || snapshot.Home["version"] != "home-world-model-v1" {
		t.Fatalf("home projection or legacy revision missing: %+v", snapshot)
	}
	entities, ok := snapshot.Home["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("home entities not projected: %#v", snapshot.Home)
	}
}
