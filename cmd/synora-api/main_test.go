package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"synora/pkg/contract"
)

type fakeCore struct {
	snapshot *contract.PublicSnapshot
	state    *contract.PublicSnapshot
	err      error
}

func (f fakeCore) Snapshot() (*contract.PublicSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.snapshot, nil
}

func (f fakeCore) State() (*contract.PublicSnapshot, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.state, nil
}

func TestHandleSnapshotReturnsPublicSnapshot(t *testing.T) {
	core := fakeCore{
		snapshot: &contract.PublicSnapshot{
			System:      map[string]any{"last_state": "idle"},
			Devices:     []map[string]any{{"id": "camera-1", "last_seen": nil}},
			Residents:   []map[string]any{},
			Nodes:       []map[string]any{},
			Events:      []map[string]any{},
			Automations: []map[string]any{},
			Cameras:     []map[string]any{},
			Tracks:      []map[string]any{},
			Clusters:    []map[string]any{},
			Clips:       []map[string]any{},
			Presence:    []map[string]any{},
			Identities:  []map[string]any{},
			Metrics:     map[string]any{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	rec := httptest.NewRecorder()

	handleSnapshot(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	for _, key := range []string{"state_store", "device", "event", "automation"} {
		if _, ok := body[key]; ok {
			t.Fatalf("snapshot exposes legacy/internal key %q in %s", key, rec.Body.String())
		}
	}
	for _, key := range []string{"devices", "events", "automations"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("snapshot missing key %q in %s", key, rec.Body.String())
		}
	}
}

func TestHandleStateReturnsPublicSnapshot(t *testing.T) {
	core := fakeCore{
		state: &contract.PublicSnapshot{
			System:      map[string]any{"last_state": "idle"},
			Devices:     []map[string]any{},
			Residents:   []map[string]any{},
			Nodes:       []map[string]any{},
			Events:      []map[string]any{},
			Automations: []map[string]any{},
			Cameras:     []map[string]any{},
			Tracks:      []map[string]any{},
			Clusters:    []map[string]any{},
			Clips:       []map[string]any{},
			Presence:    []map[string]any{},
			Identities:  []map[string]any{},
			Metrics:     map[string]any{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	rec := httptest.NewRecorder()

	handleState(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := body["devices"]; !ok {
		t.Fatalf("state missing devices key in %s", rec.Body.String())
	}
	if _, ok := body["state_store"]; ok {
		t.Fatalf("state exposes state_store in %s", rec.Body.String())
	}
}

func TestHandleSnapshotError(t *testing.T) {
	core := fakeCore{err: errors.New("core unavailable")}
	req := httptest.NewRequest(http.MethodGet, "/api/snapshot", nil)
	rec := httptest.NewRecorder()

	handleSnapshot(core).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
