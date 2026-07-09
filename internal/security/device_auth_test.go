package security

import (
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
