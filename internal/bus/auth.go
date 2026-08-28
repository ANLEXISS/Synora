package bus

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"synora/pkg/contract"
)

// AuthConfig is the optional shared key used to authenticate a Bus peer. The
// key itself never appears in a contract or a log. KeyID makes rotation
// fail-closed: a peer using an old key is rejected by a server configured with
// the new key.
type AuthConfig struct {
	KeyID  string
	Secret string
}

func (c AuthConfig) enabled() bool {
	return strings.TrimSpace(c.KeyID) != "" && strings.TrimSpace(c.Secret) != ""
}

func (c AuthConfig) configured() bool {
	return strings.TrimSpace(c.KeyID) != "" || strings.TrimSpace(c.Secret) != ""
}

func (c AuthConfig) validate() error {
	if c.configured() && !c.enabled() {
		return fmt.Errorf("bus authentication requires both key id and secret")
	}
	return nil
}

// AuthConfigFromEnv supports deployments that provision the Bus key outside
// the repository. A file is preferred so the secret does not enter a process
// listing; the environment fallback keeps local development straightforward.
func AuthConfigFromEnv() AuthConfig {
	keyID := strings.TrimSpace(os.Getenv("SYNORA_BUS_KEY_ID"))
	secret := strings.TrimSpace(os.Getenv("SYNORA_BUS_SECRET"))
	if path := strings.TrimSpace(os.Getenv("SYNORA_BUS_SECRET_FILE")); path != "" {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) != "" {
			secret = strings.TrimSpace(string(data))
		}
	}
	return AuthConfig{KeyID: keyID, Secret: secret}
}

func authenticateMessage(msg contract.Message, cfg AuthConfig, now time.Time) (contract.Message, error) {
	if err := cfg.validate(); err != nil {
		return msg, err
	}
	if !cfg.enabled() {
		return msg, nil
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = now.UTC()
	}
	if strings.TrimSpace(msg.AuthNonce) == "" {
		msg.AuthNonce = uuid.NewString()
	}
	msg.AuthKeyID = cfg.KeyID
	msg.AuthSignature = messageSignature(msg, cfg.Secret)
	return msg, nil
}

func verifyMessageAuthentication(msg contract.Message, cfg AuthConfig, now time.Time, skew time.Duration) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	if !cfg.enabled() {
		if msg.AuthKeyID != "" || msg.AuthNonce != "" || msg.AuthSignature != "" {
			return errors.New("bus authentication is not configured")
		}
		return nil
	}
	if msg.AuthKeyID != cfg.KeyID || strings.TrimSpace(msg.AuthNonce) == "" || strings.TrimSpace(msg.AuthSignature) == "" {
		return errors.New("invalid bus authentication credentials")
	}
	if msg.Timestamp.IsZero() {
		return errors.New("authenticated message timestamp required")
	}
	if skew > 0 {
		age := now.UTC().Sub(msg.Timestamp.UTC())
		if age > skew || age < -skew {
			return errors.New("authenticated message timestamp expired")
		}
	}
	expected := messageSignature(msg, cfg.Secret)
	if !hmac.Equal([]byte(strings.ToLower(msg.AuthSignature)), []byte(expected)) {
		return errors.New("invalid bus message signature")
	}
	return nil
}

func messageSignature(msg contract.Message, secret string) string {
	unsigned := msg
	unsigned.AuthKeyID = ""
	unsigned.AuthNonce = ""
	unsigned.AuthSignature = ""
	data, err := json.Marshal(unsigned)
	if err != nil {
		return ""
	}
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}
