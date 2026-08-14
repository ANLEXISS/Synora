package rpc

import (
	"encoding/json"
	"testing"

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
