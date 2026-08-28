package state

import (
	"io"
	"os"
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

func TestPurgeClipsKeepsAnAlreadyOpenedReadConsistent(t *testing.T) {
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "clip.mp4")
	content := []byte("clip evidence")
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	store.SetClip(&ClipState{
		ID: "clip-concurrent", CameraID: "cam-1", Path: path,
		Status: contract.ClipStatusProcessed, SizeBytes: int64(len(content)),
		CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour),
	})
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := store.PurgeClips(now, ClipRetentionConfig{
		MaxAge: time.Hour, MaxCount: 10, MaxBytes: 1 << 20,
		AcknowledgedMinAge: time.Hour, MinFreeBytes: 1,
	})
	if err != nil || len(removed) != 1 || removed[0] != "clip-concurrent" {
		file.Close()
		t.Fatalf("purge removed=%v err=%v", removed, err)
	}
	readContent, err := io.ReadAll(file)
	file.Close()
	if err != nil || string(readContent) != string(content) {
		t.Fatalf("opened media read changed after purge content=%q err=%v", readContent, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("purged path remains: %v", err)
	}
	clip, ok := store.Clip("clip-concurrent")
	if !ok || clip.Status != contract.ClipStatusExpired {
		t.Fatalf("purged metadata status=%#v ok=%t", clip, ok)
	}
}
