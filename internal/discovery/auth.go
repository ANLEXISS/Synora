package discovery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

func (m *Manager) VerifyCameraRequest(
	r *http.Request,
	bodyHash string,
) error {

	device := r.Header.Get("X-Synora-Device")
	tsStr := r.Header.Get("X-Synora-Timestamp")
	sig := r.Header.Get("X-Synora-Signature")

	if device == "" || tsStr == "" || sig == "" {
		return fmt.Errorf("missing auth headers")
	}

	secret, ok := m.auth.GetSecret(device)

	if !ok {
		return fmt.Errorf("unknown device")
	}

	ts, err := strconv.ParseInt(
		tsStr,
		10,
		64,
	)

	if err != nil {
		return fmt.Errorf("invalid timestamp")
	}

	if time.Since(time.Unix(ts, 0)) > 30*time.Second {
		return fmt.Errorf("timestamp expired")
	}

	payload := device + tsStr + bodyHash

	h := hmac.New(
		sha256.New,
		[]byte(secret),
	)

	h.Write([]byte(payload))

	expected := hex.EncodeToString(
		h.Sum(nil),
	)

	if !hmac.Equal(
		[]byte(expected),
		[]byte(sig),
	) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}
