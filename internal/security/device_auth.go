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
	Config func() (*Config, error)
	Now    func() time.Time
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
