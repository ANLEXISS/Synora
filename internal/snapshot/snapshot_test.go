package snapshot

import (
	"testing"
	"time"

	"synora/internal/device"
	"synora/internal/event"
	"synora/internal/state"
	"synora/internal/topology"
)

func testResidentBuilder(store *state.Store) *Builder {
	return &Builder{
		State:     store,
		Devices:   device.NewRegistry(),
		Topology:  &topology.Topology{Nodes: map[string]*topology.Node{}},
		Residents: map[string]*topology.Resident{"alexis": {ID: "alexis", Name: "Alexis"}},
		Events:    event.NewStore(10),
	}
}

func residentView(views []map[string]any, id string) map[string]any {
	for _, view := range views {
		if view["id"] == id {
			return view
		}
	}
	return nil
}

func TestResidentConfigMergeDoesNotClearLastSeen(t *testing.T) {
	seenAt := time.Date(2026, 7, 11, 17, 3, 56, 742582666, time.UTC)
	store := state.NewStore()
	store.SetPresence(&state.PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		State:      "absent",
		Confidence: 0,
		LastSeen:   seenAt,
	})

	view := residentView(testResidentBuilder(store).ResidentViews(), "alexis")
	if view == nil {
		t.Fatal("resident missing from snapshot")
	}
	lastSeen, ok := view["last_seen"].(time.Time)
	if !ok || !lastSeen.Equal(seenAt) {
		t.Fatalf("config/runtime merge cleared last_seen: %#v", view)
	}
}

func TestResidentSnapshotDoesNotClearLastSeen(t *testing.T) {
	seenAt := time.Date(2026, 7, 11, 17, 3, 56, 742582666, time.UTC)
	store := state.NewStore()
	store.SetPresence(&state.PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		State:      "present",
		Location:   "zoneA.L1.chambre_enfant",
		Confidence: 0.9,
		LastSeen:   seenAt,
		ExpiresAt:  seenAt.Add(time.Minute),
	})
	builder := testResidentBuilder(store)

	first := residentView(builder.ResidentViews(), "alexis")
	if first == nil || first["state"] != "present" {
		t.Fatalf("unexpected first resident snapshot: %#v", first)
	}

	store.Cleanup(seenAt.Add(time.Minute+time.Second), state.DefaultExpirationConfig())
	second := residentView(builder.ResidentViews(), "alexis")
	if second == nil || second["state"] != "absent" || second["confidence"] != float64(0) || second["node_id"] != "" {
		t.Fatalf("unexpected absent resident snapshot: %#v", second)
	}
	lastSeen, ok := second["last_seen"].(time.Time)
	if !ok || !lastSeen.Equal(seenAt) {
		t.Fatalf("second snapshot cleared last_seen: %#v", second)
	}
}

func TestResidentSnapshotRecoversLastSeenFromIdentity(t *testing.T) {
	seenAt := time.Date(2026, 7, 11, 17, 3, 56, 742582666, time.UTC)
	store := state.NewStore()
	store.SetIdentity(&state.IdentityState{
		ID:         "alexis",
		State:      "present",
		Confidence: 0.9,
		LastSeen:   seenAt,
	})
	store.SetPresence(&state.PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		State:      "absent",
		Confidence: 0,
	})

	view := residentView(testResidentBuilder(store).ResidentViews(), "alexis")
	if view == nil {
		t.Fatal("resident missing from snapshot")
	}
	lastSeen, ok := view["last_seen"].(time.Time)
	if !ok || !lastSeen.Equal(seenAt) {
		t.Fatalf("snapshot did not recover identity last_seen: %#v", view)
	}
}
