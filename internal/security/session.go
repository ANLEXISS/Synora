package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const SessionCookieName = "synora_session"

var (
	ErrInvalidSession = errors.New("invalid session")
	ErrExpiredSession = errors.New("expired session")
)

type SessionClaims struct {
	Subject   string    `json:"sub"`
	Role      string    `json:"role"`
	CSRF      string    `json:"csrf"`
	ExpiresAt time.Time `json:"exp"`
}

func SignSession(secret []byte, claims SessionClaims) (string, error) {
	if len(secret) < 16 || strings.TrimSpace(claims.Subject) == "" || !validSessionRole(claims.Role) || strings.TrimSpace(claims.CSRF) == "" || claims.ExpiresAt.IsZero() {
		return "", ErrInvalidSession
	}
	body, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("encode session: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(encoded))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + signature, nil
}

func VerifySession(secret []byte, token string, now time.Time) (SessionClaims, error) {
	if len(secret) < 16 {
		return SessionClaims{}, ErrInvalidSession
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return SessionClaims{}, ErrInvalidSession
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(parts[0]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || subtle.ConstantTimeCompare(want, got) != 1 {
		return SessionClaims{}, ErrInvalidSession
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return SessionClaims{}, ErrInvalidSession
	}
	var claims SessionClaims
	if json.Unmarshal(body, &claims) != nil || strings.TrimSpace(claims.Subject) == "" || !validSessionRole(claims.Role) || strings.TrimSpace(claims.CSRF) == "" {
		return SessionClaims{}, ErrInvalidSession
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !claims.ExpiresAt.After(now.UTC()) {
		return SessionClaims{}, ErrExpiredSession
	}
	return claims, nil
}

func validSessionRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin", "resident", "guest":
		return true
	default:
		return false
	}
}

func RoleAllows(actual, required string) bool {
	rank := func(role string) int {
		switch strings.ToLower(strings.TrimSpace(role)) {
		case "admin":
			return 3
		case "resident":
			return 2
		case "guest":
			return 1
		default:
			return 0
		}
	}
	return rank(actual) >= rank(required) && rank(required) > 0
}
