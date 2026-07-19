package fieldtrial

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Pseudonymizer struct {
	key       []byte
	sessionID string
}

func NewPseudonymizer(sessionID string, key []byte) (*Pseudonymizer, error) {
	if !validSessionID(sessionID) || len(key) < 16 {
		return nil, ErrKeyUnavailable
	}
	return &Pseudonymizer{key: append([]byte(nil), key...), sessionID: sessionID}, nil
}

func (p *Pseudonymizer) Ref(kind, value string) string {
	if p == nil || value == "" {
		return ""
	}
	h := hmac.New(sha256.New, p.key)
	_, _ = io.WriteString(h, p.sessionID)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, kind)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, value)
	sum := h.Sum(nil)
	return "trial-ref-" + hex.EncodeToString(sum[:18])
}

func loadOrCreateKey(config Config, sessionDir string, injected []byte) ([]byte, string, error) {
	if len(injected) >= 16 {
		return append([]byte(nil), injected...), "injected", nil
	}
	path := config.PseudonymizationKeyFile
	if path == "" {
		path = filepath.Join(sessionDir, "pseudonym.key")
	}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) < 16 {
			return nil, path, ErrKeyUnavailable
		}
		return data, path, nil
	} else if !os.IsNotExist(err) {
		return nil, path, fmt.Errorf("%w: read key", ErrKeyUnavailable)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, path, fmt.Errorf("%w: generate key", ErrKeyUnavailable)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, path, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, path, fmt.Errorf("%w: create key", ErrKeyUnavailable)
	}
	if _, err = file.Write(key); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return nil, path, fmt.Errorf("%w: write key", ErrKeyUnavailable)
	}
	if closeErr != nil {
		return nil, path, closeErr
	}
	return key, path, nil
}
