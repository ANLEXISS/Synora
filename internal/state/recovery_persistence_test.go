package state

import (
	"path/filepath"
	"testing"
	"time"

	"synora/internal/recovery"
)

func TestRecoveryStatusIsPersistedAndRestoredWithoutClaimingHealth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore()
	store.SetPersistence(NewFilePersistence(path))
	status := recovery.Snapshot{
		State:            recovery.Degraded,
		Ready:            true,
		Healthy:          false,
		RecoveryComplete: true,
		Reason:           "optional worker unavailable",
		UpdatedAt:        time.Date(2026, 8, 22, 19, 0, 0, 0, time.UTC),
	}
	if err := store.SetRecoveryStatus(status); err != nil {
		t.Fatal(err)
	}
	restored := NewStore()
	restored.SetPersistence(NewFilePersistence(path))
	if _, err := restored.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	got := restored.SystemState()
	if got.LifecycleState != string(recovery.Degraded) || !got.Ready || got.Healthy || !got.RecoveryComplete || got.LifecycleReason != status.Reason {
		t.Fatalf("recovery status was not restored exactly: %#v", got)
	}
}
