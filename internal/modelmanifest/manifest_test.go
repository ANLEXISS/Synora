package modelmanifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequiredAndOptionalModels(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "yolov8.rknn"), []byte("model"), 0644); err != nil {
		t.Fatal(err)
	}
	checks := Default().Check(root)
	statuses := map[string]string{}
	for _, check := range checks {
		statuses[check.Name] = check.Status
	}
	if statuses["weapon.rknn"] != "degraded" || statuses["yolov8.rknn"] != "ok" || statuses["det_10g.rknn"] != "fatal" {
		t.Fatalf("unexpected statuses: %#v", statuses)
	}
}
