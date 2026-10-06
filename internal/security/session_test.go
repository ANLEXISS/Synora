package security

import (
	"testing"
	"time"
)

func TestSessionRoundTripAndExpiry(t *testing.T) {
	secret := []byte("session-secret-that-is-long-enough")
	now := time.Unix(100, 0).UTC()
	token, err := SignSession(secret, SessionClaims{Subject: "user-1", Role: "resident", CSRF: "csrf-1", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := VerifySession(secret, token, now)
	if err != nil || claims.Subject != "user-1" || claims.Role != "resident" || claims.CSRF != "csrf-1" {
		t.Fatalf("unexpected claims: %#v err=%v", claims, err)
	}
	if _, err := VerifySession(secret, token, now.Add(2*time.Hour)); err != ErrExpiredSession {
		t.Fatalf("expired session error = %v", err)
	}
}

func TestSessionRejectsTamperingAndInvalidRole(t *testing.T) {
	secret := []byte("session-secret-that-is-long-enough")
	now := time.Now().UTC()
	if _, err := SignSession(secret, SessionClaims{Subject: "user-1", Role: "operator", CSRF: "csrf", ExpiresAt: now.Add(time.Hour)}); err == nil {
		t.Fatal("unsupported role accepted")
	}
	token, err := SignSession(secret, SessionClaims{Subject: "user-1", Role: "guest", CSRF: "csrf", ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifySession(secret, token+"x", now); err == nil {
		t.Fatal("tampered session accepted")
	}
}

func TestRoleAllows(t *testing.T) {
	if !RoleAllows("admin", "guest") || !RoleAllows("resident", "guest") || RoleAllows("guest", "resident") || RoleAllows("unknown", "guest") {
		t.Fatal("unexpected RBAC ordering")
	}
}
