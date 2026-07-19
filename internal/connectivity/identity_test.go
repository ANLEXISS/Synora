package connectivity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestIdentityAndWireGuardRemainStable(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadOrGenerateIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrGenerateIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first.DeviceID() != second.DeviceID() || first.Fingerprint() != second.Fingerprint() || first.WireGuardPublicFingerprint() != second.WireGuardPublicFingerprint() {
		t.Fatal("identity changed after reload")
	}
	for _, name := range []string{IdentityFile, WireGuardKeyFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode=%o", name, info.Mode().Perm())
		}
	}
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 || string(data) != "{}" {
		t.Fatalf("identity should not serialize key material: %s", data)
	}
}

func TestIdentityRejectsMalformedAndSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, IdentityFile), []byte(`{"version":1,"private_key":"bad"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrGenerateIdentity(dir); err == nil {
		t.Fatal("expected malformed identity rejection")
	}
	dir = t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, IdentityFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrGenerateIdentity(dir); err == nil {
		t.Fatal("expected identity symlink rejection")
	}
}

func TestConcurrentIdentityGenerationDoesNotReplaceKey(t *testing.T) {
	dir := t.TempDir()
	results := make([]string, 8)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			identity, err := LoadOrGenerateIdentity(dir)
			if err == nil {
				results[index] = identity.DeviceID()
			}
		}(i)
	}
	wg.Wait()
	for _, result := range results {
		if result == "" || result != results[0] {
			t.Fatalf("concurrent identity mismatch: %#v", results)
		}
	}
}
