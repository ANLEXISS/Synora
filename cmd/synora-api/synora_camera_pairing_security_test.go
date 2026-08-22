package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"synora/internal/security"
)

func TestSynoraCameraPairingRequiresSignedOneTimeClaimAndRegistersIdentity(t *testing.T) {
	provider, store := newPairingFake()
	store.requirePublicKey = true
	store.requireObservedMAC = true
	registry := security.NewIdentityRegistry(filepath.Join(t.TempDir(), "identities.json"))
	if err := registry.Load(); err != nil {
		t.Fatal(err)
	}
	store.identityRegistry = registry
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	public, private, err := security.GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	encodedPublic := base64.StdEncoding.EncodeToString(public)
	qr := `{"type":"synora.camera","version":1,"device_id":"cam-secure","serial":"SYN-SECURE","model":"synora-cam-fe","setup_token":"one_time_secret","public_key":"` + encodedPublic + `"}`
	start := callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+jsonString(qr)+`}`)
	if start.Code != http.StatusOK {
		t.Fatal(start.Body.String())
	}
	var started synoraCameraPairingStartResponse
	if err := json.Unmarshal(start.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	mac := "02:00:00:00:00:10"
	timestamp := strconv.FormatInt(now.Unix(), 10)
	fingerprint := security.IdentityFingerprint(public)
	proof := security.PairingProofMessage("cam-secure", "one_time_secret", timestamp, mac, fingerprint)
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(private, proof))
	claimBody := `{"device_id":"cam-secure","setup_token":"one_time_secret","mac":"` + mac + `","public_key":"` + encodedPublic + `","timestamp":"` + timestamp + `","signature":"` + signature + `"}`
	claim := callPairing(handleSynoraCameraPairingClaim(store), http.MethodPost, "/api/devices/pairing/synora-camera/claim", claimBody)
	if claim.Code != http.StatusOK {
		t.Fatalf("signed claim status=%d body=%s", claim.Code, claim.Body.String())
	}
	// The one-time claim is consumed at first successful observation; a replay
	// cannot refresh the station allowlist or alter the observed identity.
	replay := callPairing(handleSynoraCameraPairingClaim(store), http.MethodPost, "/api/devices/pairing/synora-camera/claim", claimBody)
	if replay.Code != http.StatusNotFound {
		t.Fatalf("replayed claim status=%d body=%s", replay.Code, replay.Body.String())
	}
	confirmBody := `{"session_id":"` + started.SessionID + `","name":"Secure camera","node_id":"zoneA.L0.entree"}`
	confirmed := callPairing(handleSynoraCameraPairingConfirm(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/confirm", confirmBody)
	if confirmed.Code != http.StatusOK {
		t.Fatalf("confirm status=%d body=%s", confirmed.Code, confirmed.Body.String())
	}
	record, ok := registry.Lookup("cam-secure")
	if !ok || record.Status != security.IdentityActive || record.Fingerprint != fingerprint {
		t.Fatalf("registered identity=%#v ok=%t", record, ok)
	}
}

func TestSynoraCameraPairingRejectsUnsignedSecureClaim(t *testing.T) {
	provider, store := newPairingFake()
	store.requirePublicKey = true
	store.requireObservedMAC = true
	public, _, err := security.GenerateIdentityKey()
	if err != nil {
		t.Fatal(err)
	}
	encodedPublic := base64.StdEncoding.EncodeToString(public)
	qr := `{"type":"synora.camera","version":1,"device_id":"cam-unsigned","setup_token":"one_time_secret","public_key":"` + encodedPublic + `"}`
	start := callPairing(handleSynoraCameraPairingStart(provider, store), http.MethodPost, "/api/devices/pairing/synora-camera/start", `{"raw_code":`+jsonString(qr)+`}`)
	if start.Code != http.StatusOK {
		t.Fatal(start.Body.String())
	}
	claim := callPairing(handleSynoraCameraPairingClaim(store), http.MethodPost, "/api/devices/pairing/synora-camera/claim", `{"device_id":"cam-unsigned","setup_token":"one_time_secret","mac":"02:00:00:00:00:11","public_key":"`+encodedPublic+`","timestamp":"0","signature":"bad"}`)
	if claim.Code != http.StatusNotFound {
		t.Fatalf("unsigned claim status=%d body=%s", claim.Code, claim.Body.String())
	}
}

func jsonString(value string) string {
	encoded, _ := json.Marshal(strings.TrimSpace(value))
	return string(encoded)
}
