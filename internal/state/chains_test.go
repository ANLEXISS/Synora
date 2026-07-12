package state

import (
	"path/filepath"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestEventChainsAreVisibleAndPersistedInStateStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStore(WithPersistencePath(path))
	chain := &contract.EventChain{
		ID: "chain-1", Status: contract.EventChainOpen, StartedAt: time.Now().UTC(),
		LastSignificantEventAt: time.Now().UTC(), DangerLevel: contract.DangerHigh,
	}
	store.SetEventChain(chain)
	if got, ok := store.EventChain(chain.ID); !ok || got.DangerLevel != contract.DangerHigh {
		t.Fatalf("stored chain=%#v ok=%v", got, ok)
	}
	if err := store.SaveNow(); err != nil {
		t.Fatal(err)
	}

	restored := NewStore(WithPersistencePath(path))
	if _, err := restored.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	if got, ok := restored.EventChain(chain.ID); !ok || got.Status != contract.EventChainOpen {
		t.Fatalf("restored chain=%#v ok=%v", got, ok)
	}
}
