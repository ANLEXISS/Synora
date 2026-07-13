package rpc

import (
	"encoding/json"
	"testing"
	"time"

	"synora/internal/state"
	"synora/pkg/contract"
)

func TestSecurityModeDefaultsAndTransitions(t *testing.T) {
	store := state.NewStore()
	server := NewServer(Config{State: store})
	if got := store.SystemState().Security; got.Mode != contract.SecurityModeHome || got.Armed || got.ExpectedOccupancy != contract.ExpectedOccupancyUnknown {
		t.Fatalf("default security mode=%#v", got)
	}
	got, err := server.Handler(contract.RPCSecurityArm)(contract.Message{Payload: []byte(`{"mode":"away","reason":"empty house"}`)})
	if err != nil {
		t.Fatal(err)
	}
	mode := got.(contract.SecurityModeState)
	if mode.Mode != contract.SecurityModeAway || !mode.Armed || mode.ExpectedOccupancy != contract.ExpectedOccupancyEmpty {
		t.Fatalf("away mode=%#v", mode)
	}
	got, err = server.Handler(contract.RPCSecurityArm)(contract.Message{Payload: []byte(`{"mode":"high_security"}`)})
	if err != nil || got.(contract.SecurityModeState).Mode != contract.SecurityModeHighSecurity {
		t.Fatalf("high security mode=%#v err=%v", got, err)
	}
	got, err = server.Handler(contract.RPCSecurityDisarm)(contract.Message{Payload: []byte(`{"reason":"back home"}`)})
	if err != nil || got.(contract.SecurityModeState).Mode != contract.SecurityModeHome || got.(contract.SecurityModeState).Armed {
		t.Fatalf("disarmed mode=%#v err=%v", got, err)
	}
}

func TestSecurityModePersistsAndPublishesChange(t *testing.T) {
	path := t.TempDir() + "/state.json"
	store := state.NewStore(state.WithPersistencePath(path))
	var published []string
	server := NewServer(Config{State: store, PublishEvent: func(eventType string, _ any, _ int) { published = append(published, eventType) }})
	result, err := server.Handler(contract.RPCSecurityModeUpdate)(contract.Message{Payload: []byte(`{"mode":"high_security","duration_seconds":60,"reason":"test"}`)})
	if err != nil {
		t.Fatal(err)
	}
	mode := result.(contract.SecurityModeState)
	if mode.ExpiresAt == nil || !mode.ExpiresAt.After(time.Now()) || len(published) != 1 || published[0] != contract.EventSecurityModeChanged {
		t.Fatalf("mode=%#v published=%#v", mode, published)
	}
	reloaded := state.NewStore(state.WithPersistencePath(path))
	if _, err := reloaded.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	if got := reloaded.SystemState().Security; got.Mode != contract.SecurityModeHighSecurity || !got.Armed {
		t.Fatalf("reloaded security mode=%#v", got)
	}
	data, err := json.Marshal(reloaded.SystemState())
	if err != nil || !json.Valid(data) {
		t.Fatalf("system json err=%v", err)
	}
}
