package connectivity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contracts"
)

func TestInitialStateDisabledAndEnabledUnprovisioned(t *testing.T) {
	dir := t.TempDir()
	identity, err := LoadOrGenerateIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStateStore(filepath.Join(dir, "state"))
	status, err := store.Initialize(DefaultConfig(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != contracts.StateDisabled || status.Mode != contracts.ModeNone {
		t.Fatalf("unexpected disabled state: %+v", status)
	}
	cfg := DefaultConfig()
	cfg.Enabled = true
	status, err = NewStateStore(filepath.Join(dir, "enabled-state")).Initialize(cfg, identity)
	if err != nil {
		t.Fatal(err)
	}
	if status.State != contracts.StateUnprovisioned || status.Mode != contracts.ModeNone {
		t.Fatalf("unexpected enabled state: %+v", status)
	}
}

func TestStatePersistsAtomicallyWithNoSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "connectivity")
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	store := NewStateStore(dir)
	status := contracts.Status{SchemaVersion: 1, State: contracts.StateDegraded, Mode: contracts.ModeNone, LastTransition: now()}
	if err := store.Save(status); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, StateFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("state mode=%o", info.Mode().Perm())
	}
	data, err := os.ReadFile(filepath.Join(dir, StateFile))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) || containsSecretField(data) {
		t.Fatalf("unsafe state: %s", data)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != contracts.StateDegraded {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestStateRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, StateFile)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStateStore(dir).Load(); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func now() time.Time { return time.Now().UTC() }

func containsSecretField(data []byte) bool {
	for _, value := range []string{"private_key", "token", "password", "psk", "secret"} {
		if bytes.Contains(data, []byte(value)) {
			return true
		}
	}
	return false
}
