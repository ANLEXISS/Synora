package bus

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"synora/pkg/contract"
)

func TestBusACLRejectsSpoofAndWrongTarget(t *testing.T) {
	server := NewServerWithConfig("test", ServerConfig{Now: func() time.Time { return time.Unix(1000, 0).UTC() }})

	if err := validateMessage(contract.Message{Type: contract.EventVisionMotion, Kind: contract.KindEvent, Source: "core", Target: "core"}, "api"); err == nil {
		t.Fatal("source spoof was accepted")
	}
	if err := server.authorizeMessage(contract.Message{
		ID: "wrong-target", Type: contract.EventVisionMotion, Kind: contract.KindEvent,
		Source: "api", Target: "actions", Timestamp: time.Unix(1000, 0).UTC(),
	}, "api"); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("wrong target was accepted: %v", err)
	}
	if err := server.authorizeMessage(contract.Message{
		ID: "unknown-type", Type: "privileged.inject", Kind: contract.KindEvent,
		Source: "api", Target: "core", Timestamp: time.Unix(1000, 0).UTC(),
	}, "api"); err == nil {
		t.Fatal("unknown producer/type combination was accepted")
	}
}

func TestBusRejectsExpiredAndMutatedReplay(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	server := NewServerWithConfig("test", ServerConfig{Now: func() time.Time { return now }, ReplayWindow: time.Minute})

	expired := contract.Message{
		ID: "expired", Type: "health.check", Kind: contract.KindRPC,
		Source: "api", Target: "core", Timestamp: now.Add(-2 * time.Minute),
	}
	if err := server.authorizeMessage(expired, "api"); err == nil || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("expired message was accepted: %v", err)
	}

	first := contract.Message{
		ID: "same-id", Type: "health.check", Kind: contract.KindRPC,
		Source: "api", Target: "core", Timestamp: now, Payload: []byte(`{"probe":"one"}`),
	}
	if err := server.authorizeMessage(first, "api"); err != nil {
		t.Fatalf("first message rejected: %v", err)
	}
	mutated := first
	mutated.Payload = []byte(`{"probe":"changed"}`)
	if err := server.authorizeMessage(mutated, "api"); err == nil || !strings.Contains(err.Error(), "different payload") {
		t.Fatalf("payload mutation was not rejected: %v", err)
	}
}

func TestBusAuthenticatedNonceAndKeyRotation(t *testing.T) {
	now := time.Unix(2000, 0).UTC()
	current := AuthConfig{KeyID: "current", Secret: "current-secret"}
	server := NewServerWithConfig("test", ServerConfig{Auth: current, Now: func() time.Time { return now }, ReplayWindow: time.Minute})

	message := contract.Message{
		ID: "auth-message", Type: "health.check", Kind: contract.KindRPC,
		Source: "api", Target: "core", Timestamp: now, Payload: []byte(`{"probe":"one"}`),
	}
	authenticated, err := authenticateMessage(message, current, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.authorizeMessage(authenticated, "api"); err != nil {
		t.Fatalf("authenticated message rejected: %v", err)
	}
	if err := server.authorizeMessage(authenticated, "api"); err == nil || !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("nonce replay was accepted: %v", err)
	}

	mutated := message
	mutated.Payload = []byte(`{"probe":"changed"}`)
	mutated.AuthNonce = authenticated.AuthNonce
	mutated, err = authenticateMessage(mutated, current, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.authorizeMessage(mutated, "api"); err == nil || (!strings.Contains(err.Error(), "replayed") && !strings.Contains(err.Error(), "different payload")) {
		t.Fatalf("changed payload with reused nonce was accepted: %v", err)
	}

	old := contract.Message{
		ID: "old-key", Type: "health.check", Kind: contract.KindRPC,
		Source: "api", Target: "core", Timestamp: now,
	}
	old, err = authenticateMessage(old, AuthConfig{KeyID: "old", Secret: "old-secret"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.authorizeMessage(old, "api"); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("old key was accepted after rotation: %v", err)
	}
}

func TestBusAuthenticatedClientUsesCurrentKeyOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bus.sock")
	cfg := AuthConfig{KeyID: "current", Secret: "current-secret"}
	server := NewServerWithConfig(path, ServerConfig{Auth: cfg})
	go func() { _ = server.Start() }()
	waitFor(t, time.Second, func() bool {
		_, err := os.Stat(path)
		return err == nil
	})
	defer server.Close()

	client, err := NewClientWithConfig(path, "api", ClientConfig{Auth: cfg})
	if err != nil {
		t.Fatalf("current key client failed to register: %v", err)
	}
	defer client.Close()

	_, err = NewClientWithConfig(path, "core", ClientConfig{Auth: AuthConfig{KeyID: "old", Secret: "old-secret"}})
	if err == nil {
		t.Fatal("rotated-out key client registered")
	}
}

func TestBusProcessIdentityDoesNotTrustSourceAlone(t *testing.T) {
	if !processIdentityAllowed(peerCredential{pid: os.Getpid(), known: true}, "core") {
		t.Fatal("current test process should be accepted by the hermetic identity rule")
	}
	if processIdentityAllowed(peerCredential{pid: 1, known: true}, "core") {
		t.Fatal("unrelated process executable was accepted as core")
	}
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	if !peerOwnedByProcess(left) {
		t.Fatal("hermetic pipe identity should remain usable")
	}
}
