package cameraota

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeTransport struct {
	online     bool
	failHealth bool
	calls      []string
}

func (f *fakeTransport) Online(context.Context, string) (bool, error) {
	f.calls = append(f.calls, "online")
	return f.online, nil
}
func (f *fakeTransport) Install(context.Context, string, string) error {
	f.calls = append(f.calls, "install")
	return nil
}
func (f *fakeTransport) Reboot(context.Context, string) error {
	f.calls = append(f.calls, "reboot")
	return nil
}
func (f *fakeTransport) Health(context.Context, string) error {
	f.calls = append(f.calls, "health")
	if f.failHealth {
		return errors.New("health failed")
	}
	return nil
}
func (f *fakeTransport) MarkGood(context.Context, string) error {
	f.calls = append(f.calls, "good")
	return nil
}
func (f *fakeTransport) MarkBad(context.Context, string) error {
	f.calls = append(f.calls, "bad")
	return nil
}

func cameraFixture(t *testing.T, online bool) (*Manager, Manifest, string, *fakeTransport) {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "camera.img")
	data := []byte("zero-3w image")
	if err := os.WriteFile(bundle, data, 0o600); err != nil {
		t.Fatal(err)
	}
	public, private, _ := ed25519.GenerateKey(nil)
	sum := sha256.Sum256(data)
	manifest := Manifest{SchemaVersion: ManifestSchemaVersion, Version: "2.0.0", Model: "synora-zero-3w", MinBootloader: "1.0.0", Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	if err := manifest.Sign(private); err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{online: online}
	manager := New(filepath.Join(t.TempDir(), "ota"), public, transport)
	manager.SetStabilityWindow(0)
	return manager, manifest, bundle, transport
}

func TestOfflineCameraUpdateQueuesAndRecoveryImageIsAtomic(t *testing.T) {
	manager, manifest, bundle, transport := cameraFixture(t, false)
	result, err := manager.Apply(context.Background(), "cam-01", bundle, manifest, "synora-zero-3w", "1.1.0")
	if err != nil || !result.Queued || result.Phase != PhasePending {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if len(transport.calls) != 1 {
		t.Fatalf("offline camera contacted unexpectedly: %v", transport.calls)
	}
	recovery := filepath.Join(t.TempDir(), "recovery", "camera.img")
	if err := manager.PrepareRecoveryImage(recovery, bundle, manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(recovery); err != nil {
		t.Fatal(err)
	}
}

func TestCameraUpdateAndRollbackRepeatably(t *testing.T) {
	manager, manifest, bundle, transport := cameraFixture(t, true)
	result, err := manager.Apply(context.Background(), "cam-01", bundle, manifest, "synora-zero-3w", "1.1.0")
	if err != nil || result.Phase != PhaseGood {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	transport.failHealth = true
	if _, err := manager.Apply(context.Background(), "cam-01", bundle, manifest, "synora-zero-3w", "1.1.0"); err == nil {
		t.Fatal("unhealthy repeated update accepted")
	}
	if transport.calls[len(transport.calls)-1] != "bad" {
		t.Fatalf("rollback was not requested: %v", transport.calls)
	}
}

func TestInterruptedCameraUpdateRecoversWithMarkBad(t *testing.T) {
	manager, manifest, bundle, transport := cameraFixture(t, true)
	if err := manager.save(Record{SchemaVersion: 1, DeviceID: "cam-01", Bundle: bundle, Version: manifest.Version, Phase: PhaseRebooting}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Recover(context.Background(), "cam-01"); err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 1 || transport.calls[0] != "bad" {
		t.Fatalf("calls=%v", transport.calls)
	}
}

func TestCameraUpdateRejectsSecurityGenerationDowngrade(t *testing.T) {
	manager, manifest, bundle, transport := cameraFixture(t, true)
	manager.CurrentSecurityGeneration = 1
	if _, err := manager.Apply(context.Background(), "cam-01", bundle, manifest, "synora-zero-3w", "1.1.0"); err == nil {
		t.Fatal("camera security generation downgrade accepted")
	}
	if len(transport.calls) != 0 {
		t.Fatalf("transport contacted before downgrade rejection: %v", transport.calls)
	}
}
