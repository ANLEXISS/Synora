package security

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIdentityRegistryAuthenticatesAndRotatesWithoutPersistingPrivateKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identities.json")
	registry := NewIdentityRegistry(path)
	if err := registry.Load(); err != nil {
		t.Fatal(err)
	}
	public, private, err := GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register("cam-01", IdentityCamera, public); err != nil {
		t.Fatal(err)
	}
	message := []byte("camera challenge")
	if err := registry.Verify("cam-01", message, ed25519.Sign(private, message)); err != nil {
		t.Fatalf("valid identity signature rejected: %v", err)
	}

	rotatedPublic, rotatedPrivate, err := GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	record, err := registry.Rotate("cam-01", rotatedPublic)
	if err != nil || record.Generation != 2 || record.Status != IdentityActive {
		t.Fatalf("rotation record=%#v err=%v", record, err)
	}
	if err := registry.Verify("cam-01", message, ed25519.Sign(private, message)); err == nil {
		t.Fatal("old key remained valid after rotation")
	}
	if err := registry.Verify("cam-01", message, ed25519.Sign(rotatedPrivate, message)); err != nil {
		t.Fatalf("rotated key rejected: %v", err)
	}
	if err := registry.VerifyAtGeneration("cam-01", 1, message, ed25519.Sign(rotatedPrivate, message)); err == nil {
		t.Fatal("old identity generation remained accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), string(rotatedPrivate)) || strings.Contains(string(data), "private_key") {
		t.Fatal("identity registry persisted private key material")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("registry permissions=%v err=%v", info.Mode().Perm(), err)
	}
}

func TestIdentityRegistryRevokesAndReplacesDevices(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identities.json")
	registry := NewIdentityRegistry(path)
	registry.SetClock(func() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) })
	if err := registry.Load(); err != nil {
		t.Fatal(err)
	}
	oldPublic, oldPrivate, err := GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register("cam-old", IdentityCamera, oldPublic); err != nil {
		t.Fatal(err)
	}
	newPublic, newPrivate, err := GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := registry.Replace("cam-old", "cam-new", newPublic)
	if err != nil || replacement.Generation != 2 {
		t.Fatalf("replacement=%#v err=%v", replacement, err)
	}
	message := []byte("replacement challenge")
	if err := registry.Verify("cam-old", message, ed25519.Sign(oldPrivate, message)); err == nil {
		t.Fatal("replaced identity remained valid")
	}
	if err := registry.Verify("cam-new", message, ed25519.Sign(newPrivate, message)); err != nil {
		t.Fatalf("replacement identity rejected: %v", err)
	}
	if err := registry.Revoke("cam-new", "stolen camera"); err != nil {
		t.Fatal(err)
	}
	if err := registry.Verify("cam-new", message, ed25519.Sign(newPrivate, message)); err == nil {
		t.Fatal("revoked identity remained valid")
	}
}

func TestIdentityRegistryFailsClosedOnCorruptOrBroadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identities.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"identities":{`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := NewIdentityRegistry(path).Load(); err == nil {
		t.Fatal("corrupt identity registry accepted")
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"identities":{}}`), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if err := NewIdentityRegistry(path).Load(); err == nil {
		t.Fatal("broad identity registry permissions accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(`{"version":1,"identities":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := NewIdentityRegistry(path).Load(); err == nil {
		t.Fatal("symlink identity registry accepted")
	}
}
