package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTrackingIsDefensiveAndPersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	first := NewStore(WithPersistencePath(path))
	first.SetResidentTrack(&ResidentTrack{ResidentID: "resident-a", LastNodeID: "entry", LastSeen: at, UpdatedAt: at, ExpiresAt: at.Add(time.Minute), Confidence: .9})
	first.SetEntityTrack(&EntityTrack{ID: "entity-track-a", TrackID: "track-a", Kind: "unknown", LastSeen: at, UpdatedAt: at, ExpiresAt: at.Add(time.Minute), Confidence: .8})
	first.SetInputCursor("vision-epoch-a", 12)
	reloaded := NewStore(WithPersistencePath(path))
	if _, err := reloaded.LoadPersisted(); err != nil {
		t.Fatalf("load state: %v", err)
	}
	resident, ok := reloaded.ResidentTrack("resident-a")
	if !ok || resident == nil || resident.LastNodeID != "entry" {
		t.Fatalf("resident track did not persist: %#v", resident)
	}
	entity, ok := reloaded.EntityTrack("entity-track-a")
	if !ok || entity == nil || entity.TrackID != "track-a" {
		t.Fatalf("entity track did not persist: %#v", entity)
	}
	epoch, sequence := reloaded.InputCursor()
	if epoch != "vision-epoch-a" || sequence != 12 {
		t.Fatalf("input cursor did not persist: %q/%d", epoch, sequence)
	}
	resident.LastNodeID = "mutated-copy"
	stored, _ := reloaded.ResidentTrack("resident-a")
	if stored.LastNodeID != "entry" {
		t.Fatal("ResidentTrack accessor returned mutable internal state")
	}
}

func TestEntityTrackExpiryIsDeterministic(t *testing.T) {
	store := NewStore()
	at := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	store.SetEntityTrack(&EntityTrack{ID: "entity-expire", LastSeen: at, ExpiresAt: at.Add(time.Second)})
	result := store.Cleanup(at.Add(2*time.Second), DefaultExpirationConfig())
	if len(result.Deleted["entity_tracks"]) != 1 || result.Deleted["entity_tracks"][0] != "entity-expire" {
		t.Fatalf("unexpected entity cleanup: %#v", result)
	}
}

func TestTrackingRejectsDelayedClusterAndPresenceUpdates(t *testing.T) {
	store := NewStore()
	newer := time.Date(2026, 8, 11, 12, 0, 10, 0, time.UTC)
	older := newer.Add(-time.Second)
	store.SetCluster(&Cluster{ID: "cluster-1", NodeID: "hall", UpdatedAt: newer, EventIDs: []string{"new"}})
	store.SetCluster(&Cluster{ID: "cluster-1", NodeID: "entry", UpdatedAt: older, EventIDs: []string{"old"}})
	cluster, _ := store.Cluster("cluster-1")
	if cluster == nil || cluster.NodeID != "hall" || len(cluster.EventIDs) != 1 || cluster.EventIDs[0] != "new" {
		t.Fatalf("delayed cluster overwrote current state: %#v", cluster)
	}
	store.SetPresence(&PresenceState{ID: "resident-1", ResidentID: "resident-1", State: "present", Location: "hall", LastSeen: newer})
	store.SetPresence(&PresenceState{ID: "resident-1", ResidentID: "resident-1", State: "present", Location: "entry", LastSeen: older})
	presence, _ := store.PresenceState("resident-1")
	if presence == nil || presence.Location != "hall" || !presence.LastSeen.Equal(newer) {
		t.Fatalf("delayed presence overwrote current state: %#v", presence)
	}
}
