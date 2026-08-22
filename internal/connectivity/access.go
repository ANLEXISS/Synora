package connectivity

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"synora/internal/configfile"
)

const (
	AccessRegistryVersion = 1
	PeerActive            = "active"
	PeerRevoked           = "revoked"
)

type AccessOwner struct {
	ID                 string `json:"id"`
	IdentityPublicKey  string `json:"identity_public_key"`
	WireGuardPublicKey string `json:"wireguard_public_key"`
}

type AccessPeer struct {
	ID                 string     `json:"id"`
	IdentityPublicKey  string     `json:"identity_public_key"`
	WireGuardPublicKey string     `json:"wireguard_public_key"`
	VirtualAddress     string     `json:"virtual_address,omitempty"`
	Status             string     `json:"status"`
	Generation         uint64     `json:"generation"`
	AddedAt            time.Time  `json:"added_at"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
}

// RendezvousRecord is deliberately metadata-only. A rendezvous service may
// carry this record, but it never receives private keys, session cookies,
// camera media, resident data, or arbitrary application payloads.
type RendezvousRecord struct {
	PeerID             string    `json:"peer_id"`
	IdentityPublicKey  string    `json:"identity_public_key"`
	WireGuardPublicKey string    `json:"wireguard_public_key"`
	Endpoint           string    `json:"endpoint"`
	Generation         uint64    `json:"generation"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type OwnerAuthorization struct {
	Action     string `json:"action"`
	Generation uint64 `json:"generation"`
	Signature  string `json:"signature"`
}

type accessRegistryDisk struct {
	Version    int                   `json:"version"`
	Generation uint64                `json:"generation"`
	Owner      AccessOwner           `json:"owner"`
	Peers      map[string]AccessPeer `json:"peers"`
	ResetAt    *time.Time            `json:"reset_at,omitempty"`
}

type AccessRegistry struct {
	mu         sync.Mutex
	path       string
	now        func() time.Time
	Version    int                   `json:"version"`
	Generation uint64                `json:"generation"`
	Owner      AccessOwner           `json:"owner"`
	Peers      map[string]AccessPeer `json:"peers"`
	ResetAt    *time.Time            `json:"reset_at,omitempty"`
}

func NewAccessRegistry(path string) (*AccessRegistry, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("access registry path is required")
	}
	registry := &AccessRegistry{
		path:       filepath.Clean(path),
		now:        func() time.Time { return time.Now().UTC() },
		Version:    AccessRegistryVersion,
		Generation: 1,
		Peers:      make(map[string]AccessPeer),
	}
	if err := registry.load(); err != nil {
		return nil, err
	}
	return registry, nil
}

func (r *AccessRegistry) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	info, err := os.Lstat(r.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errors.New("inspect access registry")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("access registry must be a regular file")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("access registry permissions are too broad")
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return errors.New("read access registry")
	}
	var disk accessRegistryDisk
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&disk); err != nil {
		return errors.New("invalid access registry")
	}
	if disk.Peers == nil {
		disk.Peers = make(map[string]AccessPeer)
	}
	r.Version, r.Generation, r.Owner, r.Peers, r.ResetAt = disk.Version, disk.Generation, disk.Owner, disk.Peers, disk.ResetAt
	return r.validateLocked()
}

func (r *AccessRegistry) validateLocked() error {
	if r.Version != AccessRegistryVersion || r.Generation == 0 {
		return errors.New("unsupported access registry version")
	}
	if r.Owner.ID != "" {
		if err := validateOwner(r.Owner); err != nil {
			return err
		}
	}
	for id, peer := range r.Peers {
		if id != peer.ID || !validIdentifier(id) || peer.Generation == 0 || (peer.Status != PeerActive && peer.Status != PeerRevoked) {
			return errors.New("invalid access peer")
		}
		if _, err := decodeKey(peer.IdentityPublicKey, ed25519.PublicKeySize); err != nil {
			return errors.New("invalid access peer identity key")
		}
		if _, err := decodeKey(peer.WireGuardPublicKey, 32); err != nil {
			return errors.New("invalid access peer WireGuard key")
		}
	}
	return nil
}

func validateOwner(owner AccessOwner) error {
	if !validIdentifier(owner.ID) {
		return errors.New("invalid access owner")
	}
	if _, err := decodeKey(owner.IdentityPublicKey, ed25519.PublicKeySize); err != nil {
		return errors.New("invalid access owner identity key")
	}
	if _, err := decodeKey(owner.WireGuardPublicKey, 32); err != nil {
		return errors.New("invalid access owner WireGuard key")
	}
	return nil
}

func validIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, "/\\\x00")
}

func decodeKey(value string, size int) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(decoded) != size {
		return nil, errors.New("invalid public key")
	}
	return decoded, nil
}

func (r *AccessRegistry) EnrollOwner(owner AccessOwner) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Owner.ID != "" {
		return errors.New("access owner already enrolled")
	}
	if err := validateOwner(owner); err != nil {
		return err
	}
	r.Owner = owner
	r.Generation++
	return r.saveLocked()
}

func (r *AccessRegistry) RegisterPeer(peer AccessPeer, auth OwnerAuthorization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorizeOwnerLocked(auth, "register-peer:"+peer.ID); err != nil {
		return err
	}
	if _, exists := r.Peers[peer.ID]; exists {
		return errors.New("access peer already exists")
	}
	if !validIdentifier(peer.ID) {
		return errors.New("invalid access peer id")
	}
	if _, err := decodeKey(peer.IdentityPublicKey, ed25519.PublicKeySize); err != nil {
		return err
	}
	if _, err := decodeKey(peer.WireGuardPublicKey, 32); err != nil {
		return err
	}
	peer.Status = PeerActive
	peer.Generation = r.Generation + 1
	peer.AddedAt = r.currentTime()
	r.Generation++
	r.Peers[peer.ID] = peer
	return r.saveLocked()
}

func (r *AccessRegistry) RevokePeer(peerID string, auth OwnerAuthorization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorizeOwnerLocked(auth, "revoke-peer:"+peerID); err != nil {
		return err
	}
	peer, exists := r.Peers[peerID]
	if !exists {
		return errors.New("access peer not found")
	}
	if peer.Status == PeerRevoked {
		return nil
	}
	now := r.currentTime()
	peer.Status, peer.RevokedAt = PeerRevoked, &now
	peer.Generation = r.Generation + 1
	r.Generation++
	r.Peers[peerID] = peer
	return r.saveLocked()
}

func (r *AccessRegistry) RotatePeer(peerID, identityPublicKey, wireGuardPublicKey string, auth OwnerAuthorization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorizeOwnerLocked(auth, "rotate-peer:"+peerID); err != nil {
		return err
	}
	peer, exists := r.Peers[peerID]
	if !exists || peer.Status != PeerActive {
		return errors.New("access peer is not active")
	}
	if _, err := decodeKey(identityPublicKey, ed25519.PublicKeySize); err != nil {
		return err
	}
	if _, err := decodeKey(wireGuardPublicKey, 32); err != nil {
		return err
	}
	peer.IdentityPublicKey, peer.WireGuardPublicKey = identityPublicKey, wireGuardPublicKey
	peer.Generation = r.Generation + 1
	r.Generation++
	r.Peers[peerID] = peer
	return r.saveLocked()
}

func (r *AccessRegistry) TransferOwnership(owner AccessOwner, auth OwnerAuthorization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorizeOwnerLocked(auth, "transfer-ownership"); err != nil {
		return err
	}
	if err := validateOwner(owner); err != nil {
		return err
	}
	for id, peer := range r.Peers {
		if peer.Status == PeerActive {
			now := r.currentTime()
			peer.Status, peer.RevokedAt = PeerRevoked, &now
			peer.Generation = r.Generation + 1
			r.Peers[id] = peer
		}
	}
	r.Owner = owner
	r.Generation++
	return r.saveLocked()
}

func (r *AccessRegistry) FactoryReset(auth OwnerAuthorization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.authorizeOwnerLocked(auth, "factory-reset"); err != nil {
		return err
	}
	now := r.currentTime()
	r.Owner = AccessOwner{}
	r.Peers = make(map[string]AccessPeer)
	r.Generation++
	r.ResetAt = &now
	return r.saveLocked()
}

func (r *AccessRegistry) IsPeerAuthorized(peerID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	peer, ok := r.Peers[peerID]
	return ok && peer.Status == PeerActive
}

func (r *AccessRegistry) PublishRendezvous(peerID, endpoint string, expiresAt time.Time) (RendezvousRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	peer, ok := r.Peers[peerID]
	if !ok || peer.Status != PeerActive {
		return RendezvousRecord{}, errors.New("access peer is not active")
	}
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" {
		return RendezvousRecord{}, errors.New("invalid rendezvous endpoint")
	}
	if !expiresAt.After(r.currentTime()) {
		return RendezvousRecord{}, errors.New("rendezvous endpoint is expired")
	}
	return RendezvousRecord{PeerID: peer.ID, IdentityPublicKey: peer.IdentityPublicKey, WireGuardPublicKey: peer.WireGuardPublicKey, Endpoint: parsed.Scheme + "://" + parsed.Host, Generation: peer.Generation, ExpiresAt: expiresAt.UTC()}, nil
}

func (r *AccessRegistry) authorizeOwnerLocked(auth OwnerAuthorization, expectedAction string) error {
	if r.Owner.ID == "" || auth.Action != expectedAction || auth.Generation != r.Generation {
		return errors.New("owner authorization rejected")
	}
	publicKey, err := decodeKey(r.Owner.IdentityPublicKey, ed25519.PublicKeySize)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), ownerActionMessage(auth.Action, auth.Generation), mustDecodeSignature(auth.Signature)) {
		return errors.New("owner authorization rejected")
	}
	return nil
}

func ownerActionMessage(action string, generation uint64) []byte {
	return []byte(fmt.Sprintf("synora-access-v1|%s|%d", action, generation))
}

func mustDecodeSignature(value string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return decoded
}

func (r *AccessRegistry) saveLocked() error {
	if err := r.validateLocked(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(accessRegistryDisk{Version: r.Version, Generation: r.Generation, Owner: r.Owner, Peers: r.Peers, ResetAt: r.ResetAt}, "", "  ")
	if err != nil {
		return errors.New("encode access registry")
	}
	data = append(data, '\n')
	if err := configfile.WriteAtomicWithBackup(r.path, data, 0600); err != nil {
		return errors.New("persist access registry")
	}
	return os.Chmod(r.path, 0600)
}

func (r *AccessRegistry) currentTime() time.Time {
	if r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}
