package state

import (
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestPurgeRecentEventsPreservesIncidentReferences(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := NewStore(WithPersistencePath(filepath.Join(t.TempDir(), "state.json")))
	store.SetRecentEvents([]*contract.Event{
		{ID: "old-referenced", Timestamp: now.Add(-2 * time.Hour), Type: "vision.unknown"},
		{ID: "old-unreferenced", Timestamp: now.Add(-2 * time.Hour), Type: "vision.motion"},
		{ID: "fresh", Timestamp: now.Add(-time.Minute), Type: "vision.motion"},
	})
	store.SetIncident(&contract.Incident{ID: "incident-1", Status: contract.IncidentStatusAcknowledged, EventIDs: []string{"old-referenced"}, UpdatedAt: now})
	if removed := store.PurgeRecentEvents(now, time.Hour); removed != 1 {
		t.Fatalf("removed=%d, want 1", removed)
	}
	items := store.RecentEventsList()
	if len(items) != 2 || items[0].ID != "old-referenced" || items[1].ID != "fresh" {
		t.Fatalf("event references were not preserved: %#v", items)
	}
}

func TestPurgeIncidentsDetachesClipReferencesAndKeepsActive(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store := NewStore(WithPersistencePath(filepath.Join(t.TempDir(), "state.json")))
	store.SetClip(&ClipState{ID: "clip-1", IncidentIDs: []string{"old", "active"}, Status: contract.ClipStatusProcessed, UpdatedAt: now})
	store.SetIncident(&contract.Incident{ID: "old", Status: contract.IncidentStatusResolved, ClipIDs: []string{"clip-1"}, UpdatedAt: now.Add(-2 * time.Hour)})
	store.SetIncident(&contract.Incident{ID: "active", Status: contract.IncidentStatusNew, ClipIDs: []string{"clip-1"}, UpdatedAt: now.Add(-2 * time.Hour)})
	removed := store.PurgeIncidents(now, time.Hour)
	if len(removed) != 1 || removed[0] != "old" {
		t.Fatalf("removed incidents=%v", removed)
	}
	if _, ok := store.Incident("active"); !ok {
		t.Fatal("active incident was purged")
	}
	clip, ok := store.Clip("clip-1")
	if !ok || len(clip.IncidentIDs) != 1 || clip.IncidentIDs[0] != "active" {
		t.Fatalf("clip references not detached: %#v", clip)
	}
}
