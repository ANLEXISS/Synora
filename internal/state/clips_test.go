package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestClipStateRegisterTransitionReconcileAndDefensiveCopies(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cam-1", "clip-1.mp4")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("clip"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	value := &ClipState{ID: "clip-1", CameraID: "cam-1", Path: path, Status: contract.ClipStatusReady, SizeBytes: 4, EventIDs: []string{"evt-1"}, CreatedAt: time.Now().UTC()}
	registered, created, err := store.RegisterClip(value)
	if err != nil || !created || registered.Status != contract.ClipStatusReady {
		t.Fatalf("register=%#v created=%t err=%v", registered, created, err)
	}
	value.EventIDs[0] = "mutated"
	stored, ok := store.Clip("clip-1")
	if !ok || stored.EventIDs[0] != "evt-1" || stored.Path != path {
		t.Fatalf("clip copy was not defensive: %#v", stored)
	}
	if _, changed, err := store.TransitionClip("clip-1", contract.ClipStatusProcessing, ""); err != nil || !changed {
		t.Fatalf("processing transition changed=%t err=%v", changed, err)
	}
	if _, _, err := store.TransitionClip("clip-1", contract.ClipStatusReceiving, ""); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("inverse transition error=%v", err)
	}
	if _, changed, err := store.TransitionClip("clip-1", contract.ClipStatusProcessing, ""); err != nil || changed {
		t.Fatalf("idempotent transition changed=%t err=%v", changed, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if changed := store.ReconcileClipFiles(time.Now().UTC()); changed != 1 {
		t.Fatalf("missing file reconciliation changed=%d", changed)
	}
	stored, _ = store.Clip("clip-1")
	if stored.Status != contract.ClipStatusMissing || stored.FailureCode != "file_missing_or_unsafe" {
		t.Fatalf("missing status=%#v", stored)
	}
}

func TestClipRegisterRejectsDivergentDuplicateAndIndexCollision(t *testing.T) {
	store := NewStore()
	base := &ClipState{
		ID: "clip-1", CameraID: "cam-1", NodeID: "entry", ActivationID: "activation-1",
		SequenceKey: "sequence-1", ClipIndex: 0, Status: contract.ClipStatusReceiving,
		SizeBytes: 10, Checksum: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		CreatedAt: time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC),
	}
	if _, created, err := store.RegisterClip(base); err != nil || !created {
		t.Fatalf("initial clip register created=%t err=%v", created, err)
	}
	divergent := *base
	divergent.Checksum = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, _, err := store.RegisterClip(&divergent); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("divergent duplicate should conflict: %v", err)
	}
	collision := *base
	collision.ID = "clip-2"
	if _, _, err := store.RegisterClip(&collision); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("same activation/index should conflict: %v", err)
	}
	otherActivation := collision
	otherActivation.ID = "clip-3"
	otherActivation.ActivationID = "activation-2"
	if _, created, err := store.RegisterClip(&otherActivation); err != nil || !created {
		t.Fatalf("different activation should remain registerable created=%t err=%v", created, err)
	}
}

func TestClipStateLifecycleTransitionsAreExplicitAndIdempotent(t *testing.T) {
	store := NewStore()
	if _, _, err := store.TransitionClip("missing", contract.ClipStatusReady, ""); contract.APIErrorCode(err) != contract.ErrorNotFound {
		t.Fatalf("missing clip transition should be not found: %v", err)
	}
	if _, _, err := store.RegisterClip(&ClipState{ID: "clip-lifecycle", CameraID: "cam-1", Status: contract.ClipStatusReceiving}); err != nil {
		t.Fatal(err)
	}
	for _, target := range []contract.ClipStatus{contract.ClipStatusReady, contract.ClipStatusProcessing, contract.ClipStatusProcessed, contract.ClipStatusExpired} {
		if _, changed, err := store.TransitionClip("clip-lifecycle", target, ""); err != nil || !changed {
			t.Fatalf("transition to %s changed=%t err=%v", target, changed, err)
		}
	}
	if _, changed, err := store.TransitionClip("clip-lifecycle", contract.ClipStatusExpired, ""); err != nil || changed {
		t.Fatalf("expired transition should be idempotent changed=%t err=%v", changed, err)
	}
	if _, _, err := store.TransitionClip("clip-lifecycle", contract.ClipStatusReady, ""); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("expired clip should not transition back: %v", err)
	}
}

func TestClipRegisterBackfillsIncidentReferences(t *testing.T) {
	store := NewStore()
	store.SetIncident(&contract.Incident{
		ID: "incident-existing", EventIDs: []string{"event-existing"}, ClipIDs: []string{"clip-late"},
		Status: contract.IncidentStatusNew,
	})
	if _, created, err := store.RegisterClip(&ClipState{
		ID: "clip-late", CameraID: "cam-1", ActivationID: "activation-1", ClipIndex: 2,
		Status: contract.ClipStatusReady,
	}); err != nil || !created {
		t.Fatalf("late clip register created=%t err=%v", created, err)
	}
	clip, ok := store.Clip("clip-late")
	if !ok || len(clip.IncidentIDs) != 1 || clip.IncidentIDs[0] != "incident-existing" || len(clip.EventIDs) != 1 || clip.EventIDs[0] != "event-existing" {
		t.Fatalf("incident references were not backfilled: %#v ok=%t", clip, ok)
	}
}

func TestClipStorageReferencesSnapshotIsDefensive(t *testing.T) {
	store := NewStore()
	store.SetClip(&ClipState{ID: "clip-1", CameraID: "cam-1", Path: "/spool/cam-1/clip-1.mp4"})
	references := store.ClipStorageReferences()
	delete(references, "/spool/cam-1/clip-1.mp4")
	if len(store.ClipStorageReferences()) != 1 {
		t.Fatal("storage references must be returned as a defensive snapshot")
	}
}

func TestClipStateRetentionProtectsActiveIncidentEvidence(t *testing.T) {
	root := t.TempDir()
	old := time.Now().UTC().Add(-2 * time.Hour)
	store := NewStore()
	makeClip := func(id string, incidentIDs []string) string {
		path := filepath.Join(root, id+".mp4")
		if err := os.WriteFile(path, []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
		value := &ClipState{ID: id, CameraID: "cam-1", Path: path, Status: contract.ClipStatusProcessed, SizeBytes: int64(len(id)), CreatedAt: old, UpdatedAt: old, IncidentIDs: incidentIDs}
		store.SetClip(value)
		return path
	}
	active := makeClip("active", []string{"incident-active"})
	makeClip("ordinary", nil)
	store.SetIncident(&contract.Incident{ID: "incident-active", Status: contract.IncidentStatusNew, ClipIDs: []string{"active"}, UpdatedAt: old})
	removed, err := store.PurgeClips(time.Now().UTC(), ClipRetentionConfig{MaxAge: time.Hour, MaxCount: 1, MaxBytes: 1, AcknowledgedMinAge: time.Hour})
	if err != nil || len(removed) != 1 || removed[0] != "ordinary" {
		t.Fatalf("retention removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active evidence should remain: %v", err)
	}
	activeClip, _ := store.Clip("active")
	if activeClip.Status == contract.ClipStatusExpired {
		t.Fatal("active evidence metadata must remain available")
	}
}

func TestClipStateRetentionPreservesExpiredMetadata(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "clip.mp4")
	if err := os.WriteFile(path, []byte("clip"), 0600); err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	store.SetClip(&ClipState{
		ID: "clip-expired", CameraID: "cam-1", Path: path,
		Status: contract.ClipStatusProcessed, SizeBytes: 4,
		CreatedAt: time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt: time.Now().UTC().Add(-2 * time.Hour),
	})
	removed, err := store.PurgeClips(time.Now().UTC(), ClipRetentionConfig{
		MaxAge: time.Hour, MaxCount: 10, MaxBytes: 1 << 20, AcknowledgedMinAge: time.Hour,
	})
	if err != nil || len(removed) != 1 {
		t.Fatalf("purge removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expired physical clip should be removed: %v", err)
	}
	if value, ok := store.Clip("clip-expired"); !ok || value.Status != contract.ClipStatusExpired {
		t.Fatalf("expired metadata must remain queryable: %#v ok=%t", value, ok)
	}
	store.Cleanup(time.Now().UTC(), DefaultExpirationConfig())
	if _, ok := store.Clip("clip-expired"); !ok {
		t.Fatal("generic cleanup must not delete clip metadata")
	}
}

func TestClipStateRetentionPurgesOrdinaryBeforeAcknowledgedEvidence(t *testing.T) {
	root := t.TempDir()
	old := time.Now().UTC().Add(-2 * time.Hour)
	store := NewStore()
	write := func(id string) string {
		path := filepath.Join(root, id+".mp4")
		if err := os.WriteFile(path, []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
		store.SetClip(&ClipState{ID: id, CameraID: "cam-1", Path: path, Status: contract.ClipStatusProcessed, SizeBytes: int64(len(id)), CreatedAt: old, UpdatedAt: old})
		return path
	}
	ordinary := write("ordinary")
	acknowledged := write("acknowledged")
	store.SetIncident(&contract.Incident{ID: "incident-ack", Status: contract.IncidentStatusAcknowledged, ClipIDs: []string{"acknowledged"}, UpdatedAt: old})
	removed, err := store.PurgeClips(time.Now().UTC(), ClipRetentionConfig{MaxAge: time.Hour, MaxCount: 1, MaxBytes: 1, AcknowledgedMinAge: time.Hour})
	if err != nil || len(removed) != 2 || removed[0] != "ordinary" || removed[1] != "acknowledged" {
		t.Fatalf("retention order removed=%v err=%v", removed, err)
	}
	for _, path := range []string{ordinary, acknowledged} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("purged clip remains path=%s err=%v", path, err)
		}
	}
}

func TestClipListCursorPaginatesEqualTimestampsWithoutSkipping(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"clip-a", "clip-b", "clip-c"} {
		store.SetClip(&ClipState{ID: id, CameraID: "cam-1", Status: contract.ClipStatusReady, CreatedAt: now, UpdatedAt: now})
	}
	first := store.ClipsListBefore(2, time.Time{}, "")
	if len(first) != 2 || first[0].ID != "clip-c" || first[1].ID != "clip-b" {
		t.Fatalf("unexpected first page: %#v", first)
	}
	second := store.ClipsListBefore(2, first[1].UpdatedAt, first[1].ID)
	if len(second) != 1 || second[0].ID != "clip-a" {
		t.Fatalf("unexpected cursor page: %#v", second)
	}
}
