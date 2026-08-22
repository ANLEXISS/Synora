package connectivity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func accessKeyPair(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(public), private
}

func accessWireGuardKey(t *testing.T) string {
	t.Helper()
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(value)
}

func ownerAuthorization(private ed25519.PrivateKey, action string, generation uint64) OwnerAuthorization {
	signature := ed25519.Sign(private, ownerActionMessage(action, generation))
	return OwnerAuthorization{Action: action, Generation: generation, Signature: base64.StdEncoding.EncodeToString(signature)}
}

func TestAccessRegistrySignedLifecycleAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "security", "access.json")
	registry, err := NewAccessRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, ownerPrivate := accessKeyPair(t)
	owner := AccessOwner{ID: "central-1", IdentityPublicKey: ownerKey, WireGuardPublicKey: accessWireGuardKey(t)}
	if err := registry.EnrollOwner(owner); err != nil {
		t.Fatal(err)
	}
	peerKey, _ := accessKeyPair(t)
	peer := AccessPeer{ID: "camera-1", IdentityPublicKey: peerKey, WireGuardPublicKey: accessWireGuardKey(t), VirtualAddress: "10.88.0.2"}
	if err := registry.RegisterPeer(peer, ownerAuthorization(ownerPrivate, "register-peer:camera-1", registry.Generation)); err != nil {
		t.Fatal(err)
	}
	if !registry.IsPeerAuthorized("camera-1") {
		t.Fatal("registered peer is not authorized")
	}

	expires := time.Now().UTC().Add(time.Minute)
	record, err := registry.PublishRendezvous("camera-1", "https://rendezvous.synora.local:443", expires)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"private", "payload", "media", "cookie", "secret"} {
		if strings.Contains(strings.ToLower(string(data)), forbidden) {
			t.Fatalf("rendezvous record contains forbidden field %q: %s", forbidden, data)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("registry permissions=%v err=%v", info, err)
	}

	restarted, err := NewAccessRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.IsPeerAuthorized("camera-1") || restarted.Owner.ID != owner.ID {
		t.Fatalf("lifecycle did not persist: owner=%+v peers=%+v", restarted.Owner, restarted.Peers)
	}
	if err := restarted.RevokePeer("camera-1", ownerAuthorization(ownerPrivate, "revoke-peer:camera-1", restarted.Generation)); err != nil {
		t.Fatal(err)
	}
	if restarted.IsPeerAuthorized("camera-1") {
		t.Fatal("revoked peer remained authorized")
	}
	if _, err := restarted.PublishRendezvous("camera-1", "https://rendezvous.synora.local:443", expires); err == nil {
		t.Fatal("revoked peer received a rendezvous record")
	}
}

func TestAccessRegistryRotationTransferAndFactoryReset(t *testing.T) {
	registry, err := NewAccessRegistry(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, ownerPrivate := accessKeyPair(t)
	if err := registry.EnrollOwner(AccessOwner{ID: "owner-a", IdentityPublicKey: ownerKey, WireGuardPublicKey: accessWireGuardKey(t)}); err != nil {
		t.Fatal(err)
	}
	peerKey, _ := accessKeyPair(t)
	if err := registry.RegisterPeer(AccessPeer{ID: "camera-1", IdentityPublicKey: peerKey, WireGuardPublicKey: accessWireGuardKey(t)}, ownerAuthorization(ownerPrivate, "register-peer:camera-1", registry.Generation)); err != nil {
		t.Fatal(err)
	}
	rotatedKey, _ := accessKeyPair(t)
	if err := registry.RotatePeer("camera-1", rotatedKey, accessWireGuardKey(t), ownerAuthorization(ownerPrivate, "rotate-peer:camera-1", registry.Generation)); err != nil {
		t.Fatal(err)
	}
	newOwnerKey, newOwnerPrivate := accessKeyPair(t)
	newOwner := AccessOwner{ID: "owner-b", IdentityPublicKey: newOwnerKey, WireGuardPublicKey: accessWireGuardKey(t)}
	if err := registry.TransferOwnership(newOwner, ownerAuthorization(ownerPrivate, "transfer-ownership", registry.Generation)); err != nil {
		t.Fatal(err)
	}
	if registry.Owner.ID != "owner-b" || registry.IsPeerAuthorized("camera-1") {
		t.Fatal("ownership transfer did not revoke existing peer access")
	}
	if err := registry.RegisterPeer(AccessPeer{ID: "camera-2", IdentityPublicKey: mustAccessPublicKey(t), WireGuardPublicKey: accessWireGuardKey(t)}, ownerAuthorization(newOwnerPrivate, "register-peer:camera-2", registry.Generation)); err != nil {
		t.Fatal(err)
	}
	if err := registry.FactoryReset(ownerAuthorization(newOwnerPrivate, "factory-reset", registry.Generation)); err != nil {
		t.Fatal(err)
	}
	if registry.Owner.ID != "" || len(registry.Peers) != 0 || registry.ResetAt == nil {
		t.Fatalf("factory reset retained access: owner=%+v peers=%+v reset=%v", registry.Owner, registry.Peers, registry.ResetAt)
	}
}

func mustAccessPublicKey(t *testing.T) string {
	key, _ := accessKeyPair(t)
	return key
}

func TestAccessRegistryRejectsUnsafeRendezvousAndAuthorization(t *testing.T) {
	registry, err := NewAccessRegistry(filepath.Join(t.TempDir(), "access.json"))
	if err != nil {
		t.Fatal(err)
	}
	ownerKey, ownerPrivate := accessKeyPair(t)
	if err := registry.EnrollOwner(AccessOwner{ID: "owner", IdentityPublicKey: ownerKey, WireGuardPublicKey: accessWireGuardKey(t)}); err != nil {
		t.Fatal(err)
	}
	peerKey, _ := accessKeyPair(t)
	peer := AccessPeer{ID: "camera", IdentityPublicKey: peerKey, WireGuardPublicKey: accessWireGuardKey(t)}
	if err := registry.RegisterPeer(peer, ownerAuthorization(ownerPrivate, "register-peer:camera", registry.Generation)); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://user:password@relay.example", "https://relay.example/path?token=secret"} {
		if _, err := registry.PublishRendezvous("camera", endpoint, time.Now().UTC().Add(time.Minute)); err == nil {
			t.Fatalf("unsafe rendezvous endpoint accepted: %s", endpoint)
		}
	}
	if err := registry.RevokePeer("camera", OwnerAuthorization{Action: "revoke-peer:camera", Generation: registry.Generation, Signature: "bad"}); err == nil {
		t.Fatal("invalid owner authorization accepted")
	}
}
