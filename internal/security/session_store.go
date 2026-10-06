package security

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type sessionStoreRecord struct {
	TokenHash string    `json:"token_hash"`
	Claims    ClaimsDTO `json:"claims"`
}

// ClaimsDTO is the durable, non-secret part of a session. The signed cookie
// remains the bearer credential; only its hash is persisted here.
type ClaimsDTO struct {
	Subject   string    `json:"sub"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"exp"`
}

type sessionStoreDisk struct {
	SchemaVersion string               `json:"schema_version"`
	Sessions      []sessionStoreRecord `json:"sessions"`
}

type SessionStore struct {
	mu       sync.Mutex
	path     string
	sessions map[string]SessionClaims
	now      func() time.Time
}

func OpenSessionStore(path string) (*SessionStore, error) {
	store := &SessionStore{path: filepath.Clean(path), sessions: make(map[string]SessionClaims), now: func() time.Time { return time.Now().UTC() }}
	if path == "" {
		store.path = ""
		return store, nil
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return nil, fmt.Errorf("create session store directory: %w", err)
	}
	body, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, store.persistLocked()
	}
	if err != nil {
		return nil, fmt.Errorf("read session store: %w", err)
	}
	var disk sessionStoreDisk
	if err := json.Unmarshal(body, &disk); err != nil || disk.SchemaVersion != "synora.session-store/v1" {
		return nil, errors.New("session store is corrupt or has an unsupported schema")
	}
	for _, record := range disk.Sessions {
		if record.TokenHash == "" || record.Claims.Subject == "" || !validSessionRole(record.Claims.Role) {
			return nil, errors.New("session store contains an invalid record")
		}
		store.sessions[record.TokenHash] = SessionClaims{Subject: record.Claims.Subject, Role: record.Claims.Role, ExpiresAt: record.Claims.ExpiresAt}
	}
	store.pruneLocked()
	if err := store.persistLocked(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SessionStore) Register(token string, claims SessionClaims) error {
	if s == nil || token == "" || claims.Subject == "" || !validSessionRole(claims.Role) || claims.CSRF == "" || claims.ExpiresAt.IsZero() {
		return ErrInvalidSession
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	s.sessions[HashSecret(token)] = claims
	return s.persistLocked()
}

func (s *SessionStore) Active(token string, now time.Time) (SessionClaims, bool) {
	if s == nil || token == "" {
		return SessionClaims{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	claims, ok := s.sessions[HashSecret(token)]
	if !ok || !claims.ExpiresAt.After(now.UTC()) {
		if ok {
			delete(s.sessions, HashSecret(token))
			_ = s.persistLocked()
		}
		return SessionClaims{}, false
	}
	return claims, true
}

func (s *SessionStore) Revoke(token string) error {
	if s == nil || token == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, HashSecret(token))
	return s.persistLocked()
}

func (s *SessionStore) RevokeSubject(subject string) error {
	if s == nil || subject == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for tokenHash, claims := range s.sessions {
		if claims.Subject == subject {
			delete(s.sessions, tokenHash)
		}
	}
	return s.persistLocked()
}

func (s *SessionStore) Count() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked()
	return len(s.sessions)
}

func (s *SessionStore) pruneLocked() {
	now := s.now().UTC()
	for tokenHash, claims := range s.sessions {
		if !claims.ExpiresAt.After(now) {
			delete(s.sessions, tokenHash)
		}
	}
}

func (s *SessionStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	records := make([]sessionStoreRecord, 0, len(s.sessions))
	for tokenHash, claims := range s.sessions {
		records = append(records, sessionStoreRecord{TokenHash: tokenHash, Claims: ClaimsDTO{Subject: claims.Subject, Role: claims.Role, ExpiresAt: claims.ExpiresAt}})
	}
	body, err := json.MarshalIndent(sessionStoreDisk{SchemaVersion: "synora.session-store/v1", Sessions: records}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".sessions-*.tmp")
	if err != nil {
		return fmt.Errorf("create session store temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(body, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	return nil
}
