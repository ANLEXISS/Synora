package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	SessionCookieName   = "synora_session"
	DefaultSessionPath  = "/var/lib/synora/auth/sessions.json"
	DefaultSessionTTL   = 12 * time.Hour
	defaultSessionBytes = 32
)

type AuthSession struct {
	ExpiresAt time.Time
	User      AuthUser
}

type persistedSession struct {
	IDHash    string    `json:"id_hash"`
	ExpiresAt time.Time `json:"expires_at"`
	User      AuthUser  `json:"user"`
}

type sessionFile struct {
	Version          int                `json:"version"`
	TokenFingerprint string             `json:"token_fingerprint,omitempty"`
	Sessions         []persistedSession `json:"sessions"`
}

// SessionStore keeps only hashes of session identifiers on disk. The raw
// identifier exists only in the browser cookie and in the process memory.
type SessionStore struct {
	path             string
	ttl              time.Duration
	tokenFingerprint string
	now              func() time.Time

	mu       sync.Mutex
	sessions map[string]persistedSession
}

func NewSessionStore(path string, ttl time.Duration, tokenFingerprint string) (*SessionStore, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultSessionPath
	}
	if ttl <= 0 {
		ttl = DefaultSessionTTL
	}

	store := &SessionStore{
		path:             path,
		ttl:              ttl,
		tokenFingerprint: strings.TrimSpace(tokenFingerprint),
		now:              func() time.Time { return time.Now().UTC() },
		sessions:         make(map[string]persistedSession),
	}

	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SessionStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var persisted sessionFile
	if err := json.Unmarshal(data, &persisted); err != nil {
		return err
	}

	if s.tokenFingerprint != "" && persisted.TokenFingerprint != "" &&
		persisted.TokenFingerprint != s.tokenFingerprint {
		// A changed API token invalidates every persisted web session.
		s.sessions = make(map[string]persistedSession)
		return s.saveLocked()
	}

	now := s.currentTime()
	for _, session := range persisted.Sessions {
		if session.IDHash == "" || !session.ExpiresAt.After(now) {
			continue
		}
		s.sessions[session.IDHash] = session
	}
	return nil
}

func (s *SessionStore) Create(user AuthUser) (string, AuthSession, error) {
	identifier := make([]byte, defaultSessionBytes)
	if _, err := rand.Read(identifier); err != nil {
		return "", AuthSession{}, err
	}
	rawID := hex.EncodeToString(identifier)
	record := persistedSession{
		IDHash:    hashSessionID(rawID),
		ExpiresAt: s.currentTime().Add(s.ttl),
		User:      user,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[record.IDHash] = record
	if err := s.saveLocked(); err != nil {
		delete(s.sessions, record.IDHash)
		return "", AuthSession{}, err
	}
	return rawID, AuthSession{ExpiresAt: record.ExpiresAt, User: record.User}, nil
}

func (s *SessionStore) Lookup(rawID string) (AuthSession, bool) {
	hash := hashSessionID(rawID)
	if hash == "" {
		return AuthSession{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.sessions[hash]
	if !ok {
		return AuthSession{}, false
	}
	if !record.ExpiresAt.After(s.currentTime()) {
		delete(s.sessions, hash)
		_ = s.saveLocked()
		return AuthSession{}, false
	}
	return AuthSession{ExpiresAt: record.ExpiresAt, User: record.User}, true
}

func (s *SessionStore) Refresh(rawID string) (string, AuthSession, bool, error) {
	oldHash := hashSessionID(rawID)
	if oldHash == "" {
		return "", AuthSession{}, false, nil
	}

	identifier := make([]byte, defaultSessionBytes)
	if _, err := rand.Read(identifier); err != nil {
		return "", AuthSession{}, false, err
	}
	newID := hex.EncodeToString(identifier)

	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.sessions[oldHash]
	if !ok || !old.ExpiresAt.After(s.currentTime()) {
		if ok {
			delete(s.sessions, oldHash)
			_ = s.saveLocked()
		}
		return "", AuthSession{}, false, nil
	}

	record := persistedSession{
		IDHash:    hashSessionID(newID),
		ExpiresAt: s.currentTime().Add(s.ttl),
		User:      old.User,
	}
	delete(s.sessions, oldHash)
	s.sessions[record.IDHash] = record
	if err := s.saveLocked(); err != nil {
		delete(s.sessions, record.IDHash)
		s.sessions[oldHash] = old
		return "", AuthSession{}, false, err
	}
	return newID, AuthSession{ExpiresAt: record.ExpiresAt, User: record.User}, true, nil
}

func (s *SessionStore) Revoke(rawID string) error {
	hash := hashSessionID(rawID)
	if hash == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[hash]; !ok {
		return nil
	}
	delete(s.sessions, hash)
	return s.saveLocked()
}

func (s *SessionStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(s.path), 0700); err != nil {
		return err
	}

	sessions := make([]persistedSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	payload, err := json.MarshalIndent(sessionFile{
		Version:          1,
		TokenFingerprint: s.tokenFingerprint,
		Sessions:         sessions,
	}, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".sessions-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(payload); err != nil {
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
	return os.Rename(tmpName, s.path)
}

func (s *SessionStore) currentTime() time.Time {
	if s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}

func hashSessionID(rawID string) string {
	rawID = strings.TrimSpace(rawID)
	if rawID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(rawID))
	return hex.EncodeToString(sum[:])
}

func SessionIDHash(rawID string) string {
	return hashSessionID(rawID)
}

func SessionIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func RequestIsHTTPS(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https")
}

func SessionCookie(r *http.Request, rawID string, expiresAt time.Time) *http.Cookie {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    rawID,
		Path:     "/",
		HttpOnly: true,
		Secure:   RequestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		Expires:  expiresAt.UTC(),
		MaxAge:   maxAge,
	}
}

func ExpiredSessionCookie(r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   RequestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
	}
}

func ClientAddress(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}
