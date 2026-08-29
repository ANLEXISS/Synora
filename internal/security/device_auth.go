package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type DeviceVerifier struct {
	Config        func() (*Config, error)
	DeviceAllowed func(string) bool
	IdentityStore *IdentityRegistry
	Now           func() time.Time
}

func (v DeviceVerifier) VerifyRequest(r *http.Request, bodyHash string) error {
	if r == nil {
		return fmt.Errorf("request is nil")
	}
	return v.VerifyHeaders(
		r.Header.Get("X-Synora-Device"),
		r.Header.Get("X-Synora-Timestamp"),
		r.Header.Get("X-Synora-Signature"),
		bodyHash,
	)
}

func (v DeviceVerifier) VerifyHeaders(
	deviceID string,
	timestamp string,
	signature string,
	bodyHash string,
) error {
	deviceID = strings.TrimSpace(deviceID)
	timestamp = strings.TrimSpace(timestamp)
	signature = strings.TrimSpace(signature)
	bodyHash = strings.TrimSpace(bodyHash)
	if deviceID == "" || timestamp == "" || signature == "" {
		return fmt.Errorf("missing auth headers")
	}
	if v.DeviceAllowed != nil && !v.DeviceAllowed(deviceID) {
		return fmt.Errorf("unknown or unconfigured device")
	}
	if v.IdentityStore != nil {
		if err := v.IdentityStore.Reload(); err != nil {
			return fmt.Errorf("camera identity unavailable")
		}
		record, ok := v.IdentityStore.Lookup(deviceID)
		if !ok || record.Kind != IdentityCamera || record.Status != IdentityActive {
			return fmt.Errorf("camera identity is not active")
		}
	}

	cfg, err := v.Config()
	if err != nil {
		return fmt.Errorf("security config: %w", err)
	}
	cfg.Normalize()

	secretHash, ok := cfg.DeviceSecrets[deviceID]
	if !ok || strings.TrimSpace(secretHash) == "" {
		return fmt.Errorf("unknown device")
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}

	now := time.Now().UTC()
	if v.Now != nil {
		now = v.Now().UTC()
	}
	at := time.Unix(ts, 0).UTC()
	skew := cfg.TimestampSkew()
	if now.Sub(at) > skew || at.Sub(now) > skew {
		return fmt.Errorf("timestamp expired")
	}

	expected := DeviceSignature(deviceID, timestamp, bodyHash, secretHash)
	if !hmac.Equal([]byte(strings.ToLower(signature)), []byte(expected)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// DeriveDeviceTransportSecret deterministically derives the post-pairing
// transport credential from the printed secret and the camera identity. The
// printed secret is never persisted by this package; callers persist only the
// returned verifier value in their protected security configuration.
func DeriveDeviceTransportSecret(deviceID, setupToken, fingerprint string) string {
	h := hmac.New(sha256.New, []byte(strings.TrimSpace(setupToken)))
	h.Write([]byte("synora-camera-transport-v1|"))
	h.Write([]byte(strings.TrimSpace(deviceID)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.TrimSpace(fingerprint)))
	return hex.EncodeToString(h.Sum(nil))
}

func DeviceSignature(
	deviceID string,
	timestamp string,
	bodyHash string,
	secretHash string,
) string {
	payload := deviceID + timestamp + bodyHash
	h := hmac.New(sha256.New, []byte(secretHash))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}
