package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func incidentObservation(id string, at time.Time) IncidentObservation {
	return IncidentObservation{
		EventID: "evt-" + id, EventType: contract.EventVisionUnknown, Timestamp: at,
		CameraID: "cam-entry", NodeID: "entry", TrackID: "track-1", ClipID: "clip-" + id,
		SequenceKey: "sequence-1", IdentityKind: contract.IncidentIdentityUnknown,
		Score: 0.91, Confidence: 0.88, SecurityState: "intrusion", Severity: "critical",
		Cause: contract.IncidentCause{EventType: contract.EventVisionUnknown, Reason: "unknown_at_night"},
	}
}

func TestIncidentsRecordGroupsAndDeduplicatesEvidence(t *testing.T) {
	store := NewStore()
	base := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	first, created, _, err := store.RecordIncident(incidentObservation("1", base))
	if err != nil || !created {
		t.Fatalf("first observation created=%t err=%v", created, err)
	}
	secondInput := incidentObservation("2", base.Add(10*time.Second))
	secondInput.EventType = contract.EventVisionWeapon
	secondInput.ClipID = "clip-2"
	second, created, _, err := store.RecordIncident(secondInput)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("second observation should enrich first created=%t first=%#v second=%#v err=%v", created, first, second, err)
	}
	_, created, _, err = store.RecordIncident(incidentObservation("2", base.Add(10*time.Second)))
	if err != nil || created {
		t.Fatalf("replayed event should be idempotent created=%t err=%v", created, err)
	}
	item, ok := store.Incident(first.ID)
	if !ok || len(item.EventIDs) != 2 || len(item.ClipIDs) != 2 || len(item.Timeline) != 2 {
		t.Fatalf("unexpected grouped incident: %#v", item)
	}
	if item.Cause.EventType != contract.EventVisionUnknown || item.Cause.Reason == "" {
		t.Fatalf("cause should retain structured trigger facts: %#v", item.Cause)
	}
	far := incidentObservation("3", base.Add(2*time.Minute))
	_, created, _, err = store.RecordIncident(far)
	if err != nil || !created {
		t.Fatalf("same track outside grouping window should create a new incident: created=%t err=%v", created, err)
	}
}

func TestIncidentLifecycleIsBoundedAndAcknowledgementDoesNotReopen(t *testing.T) {
	store := NewStore()
	base := time.Date(2026, 8, 2, 11, 0, 0, 0, time.UTC)
	item, _, _, err := store.RecordIncident(incidentObservation("1", base))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MarkIncidentViewed(item.ID); err != nil {
		t.Fatal(err)
	}
	if viewed, _, err := store.MarkIncidentViewed(item.ID); err != nil || viewed.Status != contract.IncidentStatusViewed {
		t.Fatalf("view transition should be idempotent: %#v err=%v", viewed, err)
	}
	if acknowledged, _, err := store.AcknowledgeIncident(item.ID); err != nil || acknowledged.Status != contract.IncidentStatusAcknowledged {
		t.Fatalf("ack transition failed: %#v err=%v", acknowledged, err)
	}
	if _, _, err := store.MarkIncidentViewed(item.ID); contract.APIErrorCode(err) != contract.ErrorConflict {
		t.Fatalf("acknowledged incident should reject reverse transition, err=%v", err)
	}
	_, created, _, err := store.RecordIncident(incidentObservation("1", base))
	if err != nil || created {
		t.Fatalf("replayed acknowledged event should not reopen: created=%t err=%v", created, err)
	}
	newObservation := incidentObservation("3", base.Add(2*time.Minute))
	newObservation.SequenceKey = "sequence-2"
	_, created, _, err = store.RecordIncident(newObservation)
	if err != nil || !created {
		t.Fatalf("independent post-ack intrusion should create a new incident: created=%t err=%v", created, err)
	}
}

func TestIncidentResolutionIsDistinctFromAcknowledgement(t *testing.T) {
	store := NewStore()
	item, _, _, err := store.RecordIncident(incidentObservation("resolve", time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	acknowledged, changed, err := store.AcknowledgeIncident(item.ID)
	if err != nil || !changed || acknowledged.Status != contract.IncidentStatusAcknowledged {
		t.Fatalf("acknowledgement failed: %#v changed=%t err=%v", acknowledged, changed, err)
	}
	resolved, changed, err := store.ResolveIncident(item.ID)
	if err != nil || !changed || resolved.Status != contract.IncidentStatusResolved || resolved.ResolvedAt == nil {
		t.Fatalf("resolution failed: %#v changed=%t err=%v", resolved, changed, err)
	}
	if resolved.AcknowledgedAt == nil {
		t.Fatalf("resolution must retain acknowledgement evidence: %#v", resolved)
	}
}

func TestIncidentPersistenceLegacyCompatibilityAndDefensiveCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	first := NewStore(WithPersistencePath(path))
	item, _, _, err := first.RecordIncident(incidentObservation("1", base))
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewStore(WithPersistencePath(path))
	if summary, err := loaded.LoadPersisted(); err != nil || summary.Incidents != 1 {
		t.Fatalf("incident persistence summary=%#v err=%v", summary, err)
	}
	restored, ok := loaded.Incident(item.ID)
	if !ok || len(restored.EventIDs) != 1 || len(restored.Timeline) != 1 {
		t.Fatalf("incident did not restore: %#v", restored)
	}
	restored.EventIDs[0] = "mutated"
	restored.Timeline[0].Type = "mutated"
	again, _ := loaded.Incident(item.ID)
	if again.EventIDs[0] == "mutated" || again.Timeline[0].Type == "mutated" {
		t.Fatal("incident reads must be defensive copies")
	}

	legacyPath := filepath.Join(dir, "legacy.json")
	legacy := map[string]any{"version": PersistedStateVersion, "saved_at": base}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(legacyPath, data, 0o640); err != nil {
		t.Fatal(err)
	}
	legacyStore := NewStore(WithPersistencePath(legacyPath))
	if summary, err := legacyStore.LoadPersisted(); err != nil || summary.Incidents != 0 {
		t.Fatalf("legacy state without incidents should load summary=%#v err=%v", summary, err)
	}
}

func TestIncidentRetentionPurgesAcknowledgedFirst(t *testing.T) {
	store := NewStore(WithIncidentLimit(2))
	base := time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC)
	old, _, _, err := store.RecordIncident(incidentObservation("old", base))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.AcknowledgeIncident(old.ID); err != nil {
		t.Fatal(err)
	}
	active1 := incidentObservation("active-1", base.Add(time.Minute))
	active1.SequenceKey = "active-sequence-1"
	active1.TrackID = "active-track-1"
	if _, _, _, err := store.RecordIncident(active1); err != nil {
		t.Fatal(err)
	}
	active2 := incidentObservation("active-2", base.Add(3*time.Minute))
	active2.SequenceKey = "active-sequence-2"
	active2.TrackID = "active-track-2"
	if _, _, _, err := store.RecordIncident(active2); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Incident(old.ID); ok {
		t.Fatal("old acknowledged incident should be purged before active incidents")
	}
	if len(store.IncidentsList(0)) != 2 {
		t.Fatalf("incident collection should remain bounded: %#v", store.IncidentsList(0))
	}
}
