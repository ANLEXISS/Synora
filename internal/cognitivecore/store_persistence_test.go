package cognitivecore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func durableTestCore(store *UniversalStore) *Core {
	return &Core{
		Store: store,
		MLP:   fakeMLP{output: MLPOutput{DangerLabel: "high", DangerScore: .8, Action: ActionIntent{Action: "notify"}}},
		Gate:  SafetyGate{DryRun: true},
		Now:   func() time.Time { return time.Unix(100, 0).UTC() },
	}
}

func TestUniversalStoreRestartReplayAndDuplicate(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	core := durableTestCore(store)
	event := contract.Event{ID: "durable-1", Type: contract.EventVisionSegmentReadyV1, Source: "discovery", Timestamp: time.Unix(100, 0).UTC(), Payload: map[string]any{"topology": contract.VisionTopologyProtectedInterior, "human_present": true, "track_count": 1, "confidence": .9}}
	first, err := core.Process(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if first.Result.Revision == 0 || len(store.Journal()) != 1 {
		t.Fatalf("commit not durable in memory: %#v", first.Result)
	}
	restarted, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Revision() != first.Result.Revision || len(restarted.Journal()) != 1 || len(restarted.ActionOutbox()) != 1 {
		t.Fatalf("restart lost state: revision=%d journal=%d outbox=%d", restarted.Revision(), len(restarted.Journal()), len(restarted.ActionOutbox()))
	}
	duplicate, err := durableTestCore(restarted).Process(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Result.Duplicate || restarted.Revision() != first.Result.Revision {
		t.Fatalf("replay was not idempotent: %#v", duplicate.Result)
	}
	claimed, err := restarted.ClaimPendingActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected one outbox claim, got %d", len(claimed))
	}
	claimedAgain, err := restarted.ClaimPendingActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(claimedAgain) != 0 {
		t.Fatalf("outbox replayed twice: %#v", claimedAgain)
	}
	restartedAgain, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	claimedAfterRestart, err := restartedAgain.ClaimPendingActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(claimedAfterRestart) != 0 {
		t.Fatalf("claimed outbox replayed after restart: %#v", claimedAfterRestart)
	}
}

func TestUniversalStoreCrashBeforeAndAfterWALRecovery(t *testing.T) {
	for _, test := range []struct {
		name           string
		hook           PersistenceHooks
		expectRecovery bool
	}{
		{name: "before-wal", hook: PersistenceHooks{BeforeWAL: func() error { return os.ErrPermission }}, expectRecovery: false},
		{name: "after-wal", hook: PersistenceHooks{AfterWAL: func() error { return errors.New("simulated crash after WAL") }}, expectRecovery: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := OpenUniversalStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			store.SetPersistenceHooks(test.hook)
			_, err = durableTestCore(store).Process(context.Background(), contract.Event{ID: "crash-" + test.name, Type: "sensor.motion", Source: "discovery", Timestamp: time.Unix(100, 0).UTC(), Payload: map[string]any{"movement": true}})
			if err == nil {
				t.Fatal("fault injection did not fail the commit")
			}
			recovered, err := OpenUniversalStore(dir)
			if err != nil {
				t.Fatal(err)
			}
			if (len(recovered.Journal()) == 1) != test.expectRecovery {
				t.Fatalf("unexpected recovery journal=%d expected=%v", len(recovered.Journal()), test.expectRecovery)
			}
		})
	}
}

func TestUniversalStoreActionResultIsDurableEvent(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	core := durableTestCore(store)
	_, err = core.Process(context.Background(), contract.Event{ID: "action-request", Type: "sensor.motion", Source: "discovery", Timestamp: time.Unix(100, 0).UTC(), Payload: map[string]any{"movement": true}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = core.Process(context.Background(), contract.Event{ID: "action-result", Type: contract.EventActionResult, Source: "discovery", Timestamp: time.Unix(101, 0).UTC(), Payload: map[string]any{"status": "failed", "request_id": "action-request"}})
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenUniversalStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.Journal()) != 2 || len(restarted.Snapshot().ActionResults) != 1 || !restarted.Snapshot().ActionResults[0].Failed {
		t.Fatalf("action result was not replayed: %#v", restarted.Snapshot())
	}
}

func TestUniversalStoreCorruptionFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("not-json\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUniversalStore(dir); err == nil {
		t.Fatal("corrupt store opened successfully")
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte{}, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "journal.jsonl"), []byte("not-json\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenUniversalStore(dir); err == nil {
		t.Fatal("corrupt journal opened successfully")
	}
}
