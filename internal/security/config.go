package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPath               = "/etc/synora/security.yaml"
	DefaultTimestampSkew      = 30 * time.Second
	defaultPairingTTL         = 5 * time.Minute
	defaultPairingIDByteCount = 16
	defaultSecretByteCount    = 32
)

type Config struct {
	APITokenHash            string            `yaml:"api_token_hash,omitempty"`
	APIToken                string            `yaml:"api_token,omitempty"`
	AllowedOrigins          []string          `yaml:"allowed_origins"`
	DeviceSecrets           map[string]string `yaml:"device_secrets"`
	PairingEnabled          bool              `yaml:"pairing_enabled"`
	PublicSystemHealth      bool              `yaml:"public_system_health,omitempty"`
	MaxTimestampSkewSeconds int               `yaml:"max_timestamp_skew_seconds,omitempty"`
}

func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.Normalize()
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	if cfg == nil {
		return errors.New("security config is nil")
	}
	if strings.TrimSpace(path) == "" {
		path = DefaultPath
	}

	cfg.Normalize()
	persisted := *cfg
	if persisted.APITokenHash == "" && persisted.APIToken != "" {
		persisted.APITokenHash = HashSecret(persisted.APIToken)
	}
	persisted.APIToken = ""

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := yaml.Marshal(&persisted)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

func (c *Config) Normalize() {
	if c.DeviceSecrets == nil {
		c.DeviceSecrets = map[string]string{}
	}
	c.APITokenHash = strings.TrimSpace(c.APITokenHash)
	c.APIToken = strings.TrimSpace(c.APIToken)
	if c.APITokenHash == "" && c.APIToken != "" {
		c.APITokenHash = HashSecret(c.APIToken)
	}

	origins := make([]string, 0, len(c.AllowedOrigins))
	seen := map[string]struct{}{}
	for _, origin := range c.AllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin == "" {
			continue
		}
		if _, ok := seen[origin]; ok {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	c.AllowedOrigins = origins

	for id, secret := range c.DeviceSecrets {
		trimmedID := strings.TrimSpace(id)
		trimmedSecret := strings.TrimSpace(secret)
		if trimmedID == "" || trimmedSecret == "" {
			delete(c.DeviceSecrets, id)
			continue
		}
		if trimmedID != id {
			delete(c.DeviceSecrets, id)
		}
		c.DeviceSecrets[trimmedID] = trimmedSecret
	}
}

func (c *Config) VerifyAPIToken(token string) bool {
	if c == nil {
		return false
	}
	c.Normalize()
	token = strings.TrimSpace(token)
	if token == "" || c.APITokenHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare(
		[]byte(HashSecret(token)),
		[]byte(c.APITokenHash),
	) == 1
}

func (c *Config) AllowsOrigin(origin string) bool {
	if c == nil {
		return false
	}
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}
	for _, allowed := range c.AllowedOrigins {
		if allowed == origin {
			return true
		}
		if allowed == "*" {
			return true
		}
	}
	return false
}

func (c *Config) TimestampSkew() time.Duration {
	if c != nil && c.MaxTimestampSkewSeconds > 0 {
		return time.Duration(c.MaxTimestampSkewSeconds) * time.Second
	}
	return DefaultTimestampSkew
}

func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func RandomHex(byteCount int) (string, error) {
	if byteCount <= 0 {
		return "", fmt.Errorf("invalid byte count %d", byteCount)
	}
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
