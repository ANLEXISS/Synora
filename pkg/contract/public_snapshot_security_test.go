package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicSnapshotRemovesBiometricAndFaceEvents(t *testing.T) {
	snapshot := PublicSnapshotFromCoreState(map[string]any{
		"events": []any{
			map[string]any{"type": "face.embedding", "embedding": []any{1, 2, 3}},
			map[string]any{"type": "vision.identity", "payload": map[string]any{
				"embedding": []any{1, 2}, "crop": "private", "landmarks": []any{1}, "clip_path": "/private/clip",
			}},
		},
	})
	body, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(body)
	for _, forbidden := range []string{"embedding", "crop", "landmark", "private/clip", "face.embedding"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("public snapshot leaked %q: %s", forbidden, encoded)
		}
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0]["type"] != "vision.identity" {
		t.Fatalf("unexpected public event projection: %#v", snapshot.Events)
	}
}
