package security

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDeviceVerifierAcceptsValidSignature(t *testing.T) {
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	cfg := &Config{
		DeviceSecrets: map[string]string{
			"cam_01": HashSecret("device-secret"),
		},
	}
	timestamp := "1783512000"
	bodyHash := HashSecret("clip")
	signature := DeviceSignature("cam_01", timestamp, bodyHash, cfg.DeviceSecrets["cam_01"])

	verifier := DeviceVerifier{
		Config: func() (*Config, error) {
			return cfg, nil
		},
		Now: func() time.Time {
			return now
		},
	}

	if err := verifier.VerifyHeaders("cam_01", timestamp, signature, bodyHash); err != nil {
		t.Fatalf("verify valid signature: %v", err)
	}
}

func TestDeviceVerifierRequiresActivePairedCameraIdentity(t *testing.T) {
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	registry := NewIdentityRegistry(filepath.Join(t.TempDir(), "identities.json"))
	if err := registry.Load(); err != nil {
		t.Fatal(err)
	}
	public, _, err := GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register("cam_01", IdentityCamera, public); err != nil {
		t.Fatal(err)
	}
	secretHash := DeriveDeviceTransportSecret("cam_01", "printed-key", IdentityFingerprint(public))
	cfg := &Config{DeviceSecrets: map[string]string{"cam_01": secretHash}}
	timestamp := "1783512000"
	bodyHash := HashSecret("clip")
	signature := DeviceSignature("cam_01", timestamp, bodyHash, secretHash)
	verifier := DeviceVerifier{
		Config:        func() (*Config, error) { return cfg, nil },
		DeviceAllowed: func(string) bool { return true },
		IdentityStore: registry,
		Now:           func() time.Time { return now },
	}
	if err := verifier.VerifyHeaders("cam_01", timestamp, signature, bodyHash); err != nil {
		t.Fatalf("paired camera rejected: %v", err)
	}
	if err := registry.Revoke("cam_01", "operator reset"); err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyHeaders("cam_01", timestamp, DeviceSignature("cam_01", timestamp, bodyHash, secretHash), bodyHash); err == nil {
		t.Fatal("revoked camera remained authorized")
	}
}

func TestDerivedDeviceTransportSecretIsStableAndBound(t *testing.T) {
	first := DeriveDeviceTransportSecret("cam_01", "printed-key", "fingerprint-a")
	if first == "" || first != DeriveDeviceTransportSecret("cam_01", "printed-key", "fingerprint-a") {
		t.Fatal("derived secret is not deterministic")
	}
	for _, other := range []string{
		DeriveDeviceTransportSecret("cam_02", "printed-key", "fingerprint-a"),
		DeriveDeviceTransportSecret("cam_01", "other-key", "fingerprint-a"),
		DeriveDeviceTransportSecret("cam_01", "printed-key", "fingerprint-b"),
	} {
		if first == other {
			t.Fatal("derived secret is not bound to all pairing inputs")
		}
	}
}

func TestDeviceVerifierRejectsInvalidSignature(t *testing.T) {
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	cfg := &Config{
		DeviceSecrets: map[string]string{
			"cam_01": HashSecret("device-secret"),
		},
	}
	verifier := DeviceVerifier{
		Config: func() (*Config, error) {
			return cfg, nil
		},
		Now: func() time.Time {
			return now
		},
	}

	err := verifier.VerifyHeaders("cam_01", "1783512000", "bad-signature", HashSecret("clip"))
	if err == nil {
		t.Fatal("invalid signature accepted")
	}
}

func TestDeviceVerifierRejectsDeviceNotInDurableConfiguration(t *testing.T) {
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	cfg := &Config{DeviceSecrets: map[string]string{"cam_01": HashSecret("device-secret")}}
	timestamp := "1783512000"
	bodyHash := HashSecret("clip")
	signature := DeviceSignature("cam_01", timestamp, bodyHash, cfg.DeviceSecrets["cam_01"])
	verifier := DeviceVerifier{
		Config:        func() (*Config, error) { return cfg, nil },
		DeviceAllowed: func(string) bool { return false },
		Now:           func() time.Time { return now },
	}

	if err := verifier.VerifyHeaders("cam_01", timestamp, signature, bodyHash); err == nil {
		t.Fatal("deleted/unconfigured device was accepted")
	}
}

func TestDeviceVerifierRejectsExpiredTimestamp(t *testing.T) {
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	cfg := &Config{
		DeviceSecrets: map[string]string{
			"cam_01": HashSecret("device-secret"),
		},
	}
	verifier := DeviceVerifier{
		Config: func() (*Config, error) {
			return cfg, nil
		},
		Now: func() time.Time {
			return now
		},
	}

	oldTimestamp := "1783511900"
	bodyHash := HashSecret("clip")
	signature := DeviceSignature("cam_01", oldTimestamp, bodyHash, cfg.DeviceSecrets["cam_01"])
	err := verifier.VerifyHeaders("cam_01", oldTimestamp, signature, bodyHash)
	if err == nil {
		t.Fatal("expired timestamp accepted")
	}
}
