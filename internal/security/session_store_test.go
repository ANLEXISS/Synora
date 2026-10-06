package security

import (
	"testing"
	"time"
)

func TestSessionStorePersistsAndRevokes(t *testing.T) {
	path := t.TempDir() + "/sessions.json"
	store, err := OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	token := "signed-session-token"
	claims := SessionClaims{Subject: "user-1", Role: "resident", CSRF: "csrf", ExpiresAt: now.Add(time.Hour)}
	if err := store.Register(token, claims); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return now }
	if got, ok := restarted.Active(token, now); !ok || got.Subject != claims.Subject {
		t.Fatalf("persisted session missing: %#v %v", got, ok)
	}
	if err := restarted.Revoke(token); err != nil {
		t.Fatal(err)
	}
	if _, ok := restarted.Active(token, now); ok {
		t.Fatal("revoked session remained active")
	}
	final, err := OpenSessionStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if final.Count() != 0 {
		t.Fatalf("revoked session was persisted: %d", final.Count())
	}
}

func TestSessionStoreExpiresOnAccess(t *testing.T) {
	store, err := OpenSessionStore("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(100, 0).UTC()
	store.now = func() time.Time { return now }
	if err := store.Register("token", SessionClaims{Subject: "user", Role: "guest", CSRF: "csrf", ExpiresAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Active("token", now.Add(2*time.Second)); ok {
		t.Fatal("expired session remained active")
	}
}
