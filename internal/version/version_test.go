package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrFallbackWhenVersionFileMissing(t *testing.T) {
	manifest := LoadOrFallback(filepath.Join(t.TempDir(), "missing.json"))
	if manifest.ConfigSchemaVersion != 1 || manifest.BundleID != "unversioned" {
		t.Fatalf("unexpected fallback: %+v", manifest)
	}
}

func TestLoadRejectsInvalidSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "version.json")
	if err := os.WriteFile(path, []byte(`{"config_schema_version":0}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected invalid schema error")
	}
}
