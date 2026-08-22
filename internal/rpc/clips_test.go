package rpc

import (
	"encoding/json"
	"testing"
	"time"

	"synora/internal/state"
	"synora/pkg/contract"
)

func TestClipRPCListGetBoundsAndHidesPath(t *testing.T) {
	store := state.NewStore()
	store.SetClip(&state.ClipState{ID: "clip-rpc", CameraID: "cam-1", Path: "/secret/clip.mp4", Status: contract.ClipStatusExpired})
	server := NewServer(Config{State: store})
	value, err := server.Handler("clips.get")(rpcMessage(`{"id":"clip-rpc"}`))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(value)
	if string(encoded) == "" || string(encoded) == "null" || containsString(string(encoded), "/secret") {
		t.Fatalf("clip RPC exposed internal path: %s", encoded)
	}
	listed, err := server.Handler("clips.list")(rpcMessage(`{"limit":100}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.([]contract.Clip)) != 1 {
		t.Fatalf("unexpected clip list: %#v", listed)
	}
	if listed.([]contract.Clip)[0].Path != "" {
		t.Fatalf("clip list exposed internal path: %#v", listed)
	}
	if _, err := server.Handler("clips.get")(rpcMessage(`{"id":"missing"}`)); contract.APIErrorCode(err) != contract.ErrorNotFound {
		t.Fatalf("missing clip error=%v", err)
	}
}

func TestClipRPCListSupportsRecoveryCursor(t *testing.T) {
	store := state.NewStore()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"clip-a", "clip-b", "clip-c"} {
		store.SetClip(&state.ClipState{ID: id, CameraID: "cam-1", Status: contract.ClipStatusReady, CreatedAt: now, UpdatedAt: now})
	}
	server := NewServer(Config{State: store})
	value, err := server.Handler("clips.list")(rpcMessage(`{"limit":2}`))
	if err != nil {
		t.Fatal(err)
	}
	page := value.([]contract.Clip)
	if len(page) != 2 || page[1].ID != "clip-b" {
		t.Fatalf("unexpected first page: %#v", page)
	}
	payload, _ := json.Marshal(map[string]any{"limit": 2, "before_updated_at": page[1].UpdatedAt, "before_id": page[1].ID})
	value, err = server.Handler("clips.list")(rpcMessage(string(payload)))
	if err != nil {
		t.Fatal(err)
	}
	page = value.([]contract.Clip)
	if len(page) != 1 || page[0].ID != "clip-a" {
		t.Fatalf("unexpected cursor page: %#v", page)
	}
}

func containsString(value, fragment string) bool {
	return len(value) >= len(fragment) && stringIndex(value, fragment) >= 0
}

func stringIndex(value, fragment string) int {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if value[i:i+len(fragment)] == fragment {
			return i
		}
	}
	return -1
}
