package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

type faultPersistence struct {
	loadState *PersistedState
	loadErr   error
	saveErr   error
	closeErr  error
}

func (p *faultPersistence) Load() (*PersistedState, error) {
	if p.loadErr != nil {
		return emptyPersistedState(), p.loadErr
	}
	return p.loadState, nil
}

func (p *faultPersistence) Save(*PersistedState) error { return p.saveErr }
func (p *faultPersistence) Close() error               { return p.closeErr }

func TestLoadPersistedDoesNotApplyStateWhenReadOrDecodeFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0640); err != nil {
		t.Fatal(err)
	}
	store := NewStore()
	store.SetValidation(&contract.ValidationRequest{ID: "must-survive"})
	store.SetPersistence(NewFilePersistence(path))

	if _, err := store.LoadPersisted(); err == nil {
		t.Fatal("expected unsupported version error")
	}
	if _, ok := store.Validation("must-survive"); !ok {
		t.Fatal("failed persistence load must not replace live state")
	}
	health := store.PersistenceHealth()
	if health.Healthy || !strings.Contains(health.Error, "unsupported persisted state version") {
		t.Fatalf("load failure was not observable: %#v", health)
	}
}

func TestSaveFailureIsObservableAndRecoveryClearsIt(t *testing.T) {
	saveErr := errors.New("simulated disk full")
	faults := &faultPersistence{saveErr: saveErr}
	store := NewStore(WithPersistence(faults))
	store.SetValidation(&contract.ValidationRequest{ID: "validation-1", CreatedAt: time.Now().UTC()})

	if err := store.SaveNow(); !errors.Is(err, saveErr) {
		t.Fatalf("save error=%v, want %v", err, saveErr)
	}
	health := store.PersistenceHealth()
	if health.Healthy || health.Error != saveErr.Error() || health.CheckedAt.IsZero() {
		t.Fatalf("save failure was not observable: %#v", health)
	}

	faults.saveErr = nil
	if err := store.SaveNow(); err != nil {
		t.Fatalf("recovered save: %v", err)
	}
	if health := store.PersistenceHealth(); !health.Healthy || health.Error != "" {
		t.Fatalf("successful save did not clear failure: %#v", health)
	}
}

func TestAtomicRenameFailureCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.Mkdir(path, 0750); err != nil {
		t.Fatal(err)
	}
	persistence := NewFilePersistence(path)
	if err := persistence.Save(&PersistedState{Version: PersistedStateVersion}); err == nil {
		t.Fatal("rename into existing directory should fail")
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".state-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("failed atomic save leaked temporary files: %#v", matches)
	}
}

func TestDirectorySyncFailureIsReturned(t *testing.T) {
	if err := syncDir(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("directory sync failure was swallowed")
	}
}

func TestCorruptQuarantineFailureRemainsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{not-json`), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	_, err := NewFilePersistence(path).Load()
	if err == nil {
		t.Fatal("corrupt state must remain an error even when quarantine cannot be confirmed")
	}
}
