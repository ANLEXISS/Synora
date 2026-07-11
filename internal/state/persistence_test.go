package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestPersistentStoreStartsWithoutExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(WithPersistencePath(path))

	summary, err := store.LoadPersisted()
	if err != nil {
		t.Fatalf("load missing state: %v", err)
	}
	if summary != (PersistedSummary{}) {
		t.Fatalf("expected empty summary, got %#v", summary)
	}
}

func TestPersistentStoreSavesAndReloadsDurableState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first := NewStore(WithPersistencePath(path))
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	resolvedAt := now.Add(time.Minute)

	first.SetValidation(&contract.ValidationRequest{
		ID:         "validation-1",
		EventID:    "evt-1",
		Status:     contract.ValidationStatusAccepted,
		CreatedAt:  now,
		ResolvedAt: &resolvedAt,
		Evidence:   []string{"event:evt-1"},
	})
	first.SetActionResult(&contract.ActionResult{
		ID:           "ares-1",
		RequestID:    "areq-1",
		AutomationID: "auto-1",
		ActionID:     "action-1",
		Type:         "device.command",
		Status:       contract.ActionStatusSuccess,
		FinishedAt:   now,
		Data:         map[string]any{"adapter": "fake"},
	})
	first.SetClip(&ClipState{
		ID:        "clip-1",
		CameraID:  "cam-1",
		EventID:   "evt-1",
		Path:      "/var/lib/synora/clips/clip-1.mp4",
		CreatedAt: now,
		UpdatedAt: now,
	})
	first.SetRecentEvents([]*contract.Event{{
		ID:        "evt-1",
		Type:      contract.EventVisionMotion,
		Source:    "vision-worker",
		Timestamp: now,
		DeviceID:  "cam-1",
		Payload:   map[string]any{"motion": true},
	}})
	if err := first.SaveBehaviorOverride("behavior-1", json.RawMessage(`{"id":"behavior-1","status":"approved"}`)); err != nil {
		t.Fatalf("save behavior override: %v", err)
	}

	second := NewStore(WithPersistencePath(path))
	summary, err := second.LoadPersisted()
	if err != nil {
		t.Fatalf("reload state: %v", err)
	}
	if summary.Events != 1 || summary.Clips != 1 || summary.Validations != 1 || summary.ActionResults != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
	if _, ok := second.Validation("validation-1"); !ok {
		t.Fatal("validation should reload")
	}
	if _, ok := second.Clip("clip-1"); !ok {
		t.Fatal("clip should reload")
	}
	if results := second.ActionResultsList(); len(results) != 1 || results[0].Data["adapter"] != "fake" {
		t.Fatalf("action result should reload with data: %#v", results)
	}
	if events := second.RecentEventsList(); len(events) != 1 || events[0].Payload["motion"] != true {
		t.Fatalf("events should reload defensively: %#v", events)
	}
	if override, ok := second.BehaviorOverride("behavior-1"); !ok || !json.Valid(override) {
		t.Fatalf("behavior override should reload: %s ok=%v", override, ok)
	}
}

func TestPersistentStoreSavesAndReloadsRuntimePresence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	first := NewStore(WithPersistencePath(path))
	first.SetPresence(&PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		Location:   "zoneA.L0.entree",
		Confidence: 0.92,
		State:      "present",
		CreatedAt:  now,
		UpdatedAt:  now,
		LastSeen:   now,
		ExpiresAt:  now.Add(DefaultPresenceTTL),
	})

	second := NewStore(WithPersistencePath(path))
	if _, err := second.LoadPersisted(); err != nil {
		t.Fatalf("reload runtime presence: %v", err)
	}
	presence, ok := second.PresenceState("alexis")
	if !ok || presence == nil {
		t.Fatal("runtime presence should reload")
	}
	if presence.State != "present" || presence.Location != "zoneA.L0.entree" || !presence.LastSeen.Equal(now) {
		t.Fatalf("unexpected reloaded runtime presence: %#v", presence)
	}
}

func TestResidentPersistenceKeepsLastSeenWhenAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	lastSeen := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := NewStore(WithPersistencePath(path))
	store.SetPresence(&PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		Location:   "zoneA.L0.entree",
		Confidence: 0.92,
		State:      "present",
		CreatedAt:  lastSeen,
		UpdatedAt:  lastSeen,
		LastSeen:   lastSeen,
		ExpiresAt:  lastSeen.Add(DefaultPresenceTTL),
	})

	cleanupAt := lastSeen.Add(DefaultPresenceTTL + time.Second)
	result := store.Cleanup(cleanupAt, DefaultExpirationConfig())
	if len(result.Deleted["presence"]) != 1 || result.Deleted["presence"][0] != "alexis" {
		t.Fatalf("expected expired presence result, got %#v", result)
	}
	presence, ok := store.PresenceState("alexis")
	if !ok || presence == nil {
		t.Fatal("expired presence should remain as an absent runtime record")
	}
	if presence.State != "absent" || presence.Location != "" || presence.Confidence != 0 || !presence.LastSeen.Equal(lastSeen) {
		t.Fatalf("expiration should preserve last_seen: %#v", presence)
	}

	reloaded := NewStore(WithPersistencePath(path))
	if _, err := reloaded.LoadPersisted(); err != nil {
		t.Fatalf("reload expired presence: %v", err)
	}
	presence, ok = reloaded.PresenceState("alexis")
	if !ok || presence == nil || presence.State != "absent" || !presence.LastSeen.Equal(lastSeen) {
		t.Fatalf("expired state should persist last_seen: %#v", presence)
	}
}

func TestResidentAbsentKeepsLastSeen(t *testing.T) {
	lastSeen := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	store := NewStore()
	store.SetPresence(&PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		State:      "present",
		Confidence: 0.9,
		LastSeen:   lastSeen,
	})

	store.SetPresence(&PresenceState{
		ID:         "alexis",
		ResidentID: "alexis",
		State:      "absent",
		Confidence: 0,
		Location:   "",
	})

	presence, ok := store.PresenceState("alexis")
	if !ok || presence == nil {
		t.Fatal("expected resident presence after absent update")
	}
	if presence.State != "absent" || presence.Confidence != 0 || !presence.LastSeen.Equal(lastSeen) {
		t.Fatalf("absent update erased runtime history: %#v", presence)
	}
}

func TestUserConfigurationMutationCreatesStateBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	store := NewStore(WithPersistencePath(path))
	store.SetValidation(&contract.ValidationRequest{ID: "existing", Status: contract.ValidationStatusPending})
	if err := store.SaveValidation(&contract.ValidationRequest{ID: "user", Status: contract.ValidationStatusApproved, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(dir, "backups", "state.*.json"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("state backup missing: %v err=%v", backups, err)
	}
}

func TestPersistentStoreLimitsActionResultsAfterReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first := NewStore(WithPersistencePath(path))
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 205; i++ {
		id := "result-" + time.Duration(i).String()
		first.SetActionResult(&contract.ActionResult{
			ID:         id,
			RequestID:  "request-" + time.Duration(i).String(),
			Status:     contract.ActionStatusSuccess,
			FinishedAt: base.Add(time.Duration(i) * time.Second),
		})
	}

	second := NewStore(WithPersistencePath(path))
	if _, err := second.LoadPersisted(); err != nil {
		t.Fatalf("reload state: %v", err)
	}
	items := second.Snapshot("action_results")
	if len(items) != maxActionResults {
		t.Fatalf("expected %d action results after reload, got %d", maxActionResults, len(items))
	}
	if _, ok := items["result-0s"]; ok {
		t.Fatal("oldest action result should not reload after trim")
	}
}

func TestPersistentStoreCorruptFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{not-json`), 0640); err != nil {
		t.Fatal(err)
	}

	store := NewStore(WithPersistencePath(path))
	summary, err := store.LoadPersisted()
	if err == nil {
		t.Fatal("expected corrupt file error")
	}
	if summary != (PersistedSummary{}) {
		t.Fatalf("corrupt state should load empty summary, got %#v", summary)
	}
	matches, err := filepath.Glob(path + ".corrupt.*")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("corrupt state file should be renamed, matches=%#v", matches)
	}
}

func TestPersistentStoreUnknownVersionReturnsCleanError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0640); err != nil {
		t.Fatal(err)
	}

	store := NewStore(WithPersistencePath(path))
	if _, err := store.LoadPersisted(); err == nil {
		t.Fatal("expected unknown version error")
	}
}

func TestFilePersistenceAtomicSaveLeavesValidFinalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(WithPersistencePath(path))
	store.SetValidation(&contract.ValidationRequest{
		ID:        "validation-1",
		Status:    contract.ValidationStatusPending,
		CreatedAt: time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC),
	})
	if err := store.SaveNow(); err != nil {
		t.Fatalf("save now: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted PersistedState
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("final state file should be valid JSON: %v\n%s", err, data)
	}
	if persisted.Version != PersistedStateVersion || persisted.Validations["validation-1"].ID != "validation-1" {
		t.Fatalf("unexpected persisted file: %#v", persisted)
	}
}

func TestRestoredStateVisibleInPublicSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	first := NewStore(WithPersistencePath(path))
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	first.SetValidation(&contract.ValidationRequest{ID: "validation-1", Status: contract.ValidationStatusPending, CreatedAt: now})
	first.SetActionResult(&contract.ActionResult{ID: "ares-1", RequestID: "areq-1", Status: contract.ActionStatusSuccess, FinishedAt: now})
	first.SetClip(&ClipState{ID: "clip-1", CameraID: "cam-1", CreatedAt: now})
	first.SetRecentEvents([]*contract.Event{{ID: "evt-1", Type: contract.EventVisionMotion, Timestamp: time.Time{}}})

	second := NewStore(WithPersistencePath(path))
	if _, err := second.LoadPersisted(); err != nil {
		t.Fatalf("reload state: %v", err)
	}
	public := contract.PublicSnapshotFromCoreState(map[string]any{
		"state_store": map[string]any{
			"events":         second.Snapshot("events"),
			"clips":          second.Snapshot("clips"),
			"validations":    second.Snapshot("validations"),
			"action_results": second.Snapshot("action_results"),
		},
	})

	if len(public.Events) != 1 || public.Events[0]["id"] != "evt-1" || public.Events[0]["timestamp"] != nil {
		t.Fatalf("restored event should be visible with nil zero timestamp: %#v", public.Events)
	}
	if len(public.Clips) != 1 || public.Clips[0]["id"] != "clip-1" {
		t.Fatalf("restored clip should be visible: %#v", public.Clips)
	}
	if len(public.Validations) != 1 || public.Validations[0]["id"] != "validation-1" {
		t.Fatalf("restored validation should be visible: %#v", public.Validations)
	}
	if len(public.ActionResults) != 1 || public.ActionResults[0]["id"] != "ares-1" {
		t.Fatalf("restored action result should be visible: %#v", public.ActionResults)
	}
}
