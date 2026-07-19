package fieldtrial

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunPreflightUsesTemporaryPathsOnly(t *testing.T) {
	root := t.TempDir()
	keyPath := filepath.Join(root, "trial.key")
	if _, err := GenerateKey(keyPath, false); err != nil {
		t.Fatal(err)
	}
	topologyPath := filepath.Join(root, "topology.json")
	topology := `{"revision":"test-v1","captured_at":"2026-01-01T00:00:00Z","nodes":[{"id":"entry","kind":"entrance","entry_point":true},{"id":"room","kind":"room"}],"edges":[{"from":"entry","to":"room","traversal_kind":"door"}]}`
	if err := os.WriteFile(topologyPath, []byte(topology), 0o640); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.Enabled = true
	config.RootDir = filepath.Join(root, "sessions")
	config.PseudonymizationKeyFile = keyPath
	config.TopologyFile = topologyPath
	report, err := RunPreflight(context.Background(), PreflightOptions{Config: config, KeyFile: keyPath, TopologyFile: topologyPath, CognitiveConfigurationFingerprint: "sha256:test"})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Success || report.CognitiveConfigurationFingerprint != "sha256:test" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunPreflightRejectsSymlinkedKey(t *testing.T) {
	root := t.TempDir()
	realKey := filepath.Join(root, "real.key")
	if _, err := GenerateKey(realKey, false); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.key")
	if err := os.Symlink(realKey, link); err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.Enabled = true
	config.RootDir = filepath.Join(root, "sessions")
	report, err := RunPreflight(context.Background(), PreflightOptions{Config: config, KeyFile: link})
	if err != nil {
		t.Fatal(err)
	}
	if report.Success {
		t.Fatal("symlinked key passed preflight")
	}
}

func TestDeploymentManifestIsAtomicAndVersioned(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "deployment.json")
	if err := WriteDeploymentManifest(path, DeploymentManifest{PreparedAt: time.Unix(1, 0).UTC(), CognitiveConfigurationFingerprint: "sha256:cognitive", FieldTrialRoot: filepath.Join(root, "data"), PreflightPassed: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || !containsBytes(data, []byte(SchemaVersion)) {
		t.Fatal("manifest was not written with schema version")
	}
	for _, entry := range mustReadDir(t, root) {
		if entry.Name()[0] == '.' {
			t.Fatalf("temporary manifest remains: %s", entry.Name())
		}
	}
}

func containsBytes(value, needle []byte) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		match := true
		for j := range needle {
			if value[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func mustReadDir(t *testing.T, path string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}
