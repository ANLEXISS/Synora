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
