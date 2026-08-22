package security

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"synora/internal/configfile"
)

const IdentityRegistryVersion = 1

type IdentityKind string

const (
	IdentityCentral IdentityKind = "central"
	IdentityCamera  IdentityKind = "camera"
)

type IdentityStatus string

const (
	IdentityActive   IdentityStatus = "active"
	IdentityRevoked  IdentityStatus = "revoked"
	IdentityReplaced IdentityStatus = "replaced"
)

var (
	ErrIdentityNotFound   = errors.New("identity not found")
	ErrIdentityInactive   = errors.New("identity is not active")
	ErrIdentityGeneration = errors.New("identity generation mismatch")
	identityIDPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
)

type IdentityRecord struct {
	DeviceID    string         `json:"device_id"`
	Kind        IdentityKind   `json:"kind"`
	PublicKey   string         `json:"public_key"`
	Fingerprint string         `json:"fingerprint"`
	Generation  uint64         `json:"generation"`
	Status      IdentityStatus `json:"status"`
	Reason      string         `json:"reason,omitempty"`
	ReplacedBy  string         `json:"replaced_by,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	RevokedAt   *time.Time     `json:"revoked_at,omitempty"`
}

type identityRegistryDisk struct {
	Version    int                       `json:"version"`
	Identities map[string]IdentityRecord `json:"identities"`
}

type IdentityRegistry struct {
	mu         sync.Mutex
	path       string
	now        func() time.Time
	identities map[string]IdentityRecord
	loaded     bool
}

func NewIdentityRegistry(path string) *IdentityRegistry {
	return &IdentityRegistry{
		path: strings.TrimSpace(path),
		now:  func() time.Time { return time.Now().UTC() },
	}
}

func (r *IdentityRegistry) SetClock(now func() time.Time) {
	if r == nil || now == nil {
		return
	}
	r.mu.Lock()
	r.now = now
	r.mu.Unlock()
}

func (r *IdentityRegistry) Load() error {
	if r == nil {
		return errors.New("identity registry is nil")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if strings.TrimSpace(r.path) == "" {
		return errors.New("identity registry path is required")
	}
	data, err := os.ReadFile(r.path)
	if errors.Is(err, os.ErrNotExist) {
		r.identities = map[string]IdentityRecord{}
		r.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	info, err := os.Lstat(r.path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("identity registry is not a regular file")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("identity registry permissions are too broad")
	}
	var disk identityRegistryDisk
	if err := json.Unmarshal(data, &disk); err != nil || disk.Version != IdentityRegistryVersion {
		return errors.New("invalid identity registry format")
	}
	if disk.Identities == nil {
		disk.Identities = map[string]IdentityRecord{}
	}
	for id, record := range disk.Identities {
		if err := validateRecord(id, record); err != nil {
			return fmt.Errorf("invalid identity %q: %w", id, err)
		}
	}
	r.identities = cloneIdentityRecords(disk.Identities)
	r.loaded = true
	return nil
}

func GenerateIdentityKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return append(ed25519.PublicKey(nil), public...), append(ed25519.PrivateKey(nil), private...), nil
}

func IdentityFingerprint(publicKey ed25519.PublicKey) string {
	return HashSecret(string(publicKey))
}

func (r *IdentityRegistry) Register(deviceID string, kind IdentityKind, publicKey ed25519.PublicKey) (IdentityRecord, error) {
	if err := validateIdentityInput(deviceID, kind, publicKey); err != nil {
		return IdentityRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return IdentityRecord{}, err
	}
	if _, exists := r.identities[deviceID]; exists {
		return IdentityRecord{}, fmt.Errorf("identity %q already exists", deviceID)
	}
	now := r.now().UTC()
	record := IdentityRecord{
		DeviceID: deviceID, Kind: kind,
		PublicKey:   base64.StdEncoding.EncodeToString(publicKey),
		Fingerprint: IdentityFingerprint(publicKey), Generation: 1,
		Status: IdentityActive, CreatedAt: now, UpdatedAt: now,
	}
	return record, r.persistLocked(func(next map[string]IdentityRecord) { next[deviceID] = record })
}

func (r *IdentityRegistry) Rotate(deviceID string, publicKey ed25519.PublicKey) (IdentityRecord, error) {
	if err := validatePublicKey(publicKey); err != nil {
		return IdentityRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return IdentityRecord{}, err
	}
	current, ok := r.identities[deviceID]
	if !ok {
		return IdentityRecord{}, ErrIdentityNotFound
	}
	if current.Status != IdentityActive {
		return IdentityRecord{}, fmt.Errorf("%w: %s", ErrIdentityInactive, current.Status)
	}
	now := r.now().UTC()
	current.PublicKey = base64.StdEncoding.EncodeToString(publicKey)
	current.Fingerprint = IdentityFingerprint(publicKey)
	current.Generation++
	current.Reason = "rotated"
	current.UpdatedAt = now
	return current, r.persistLocked(func(next map[string]IdentityRecord) { next[deviceID] = current })
}

func (r *IdentityRegistry) Revoke(deviceID, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return err
	}
	current, ok := r.identities[deviceID]
	if !ok {
		return ErrIdentityNotFound
	}
	if current.Status == IdentityRevoked {
		return nil
	}
	now := r.now().UTC()
	current.Status = IdentityRevoked
	current.Reason = strings.TrimSpace(reason)
	current.UpdatedAt = now
	current.RevokedAt = &now
	return r.persistLocked(func(next map[string]IdentityRecord) { next[deviceID] = current })
}

func (r *IdentityRegistry) Replace(oldDeviceID, newDeviceID string, publicKey ed25519.PublicKey) (IdentityRecord, error) {
	if err := validateIdentityInput(newDeviceID, IdentityCamera, publicKey); err != nil {
		return IdentityRecord{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.ensureLoadedLocked(); err != nil {
		return IdentityRecord{}, err
	}
	old, ok := r.identities[oldDeviceID]
	if !ok {
		return IdentityRecord{}, ErrIdentityNotFound
	}
	if old.Status != IdentityActive {
		return IdentityRecord{}, fmt.Errorf("%w: %s", ErrIdentityInactive, old.Status)
	}
	if _, exists := r.identities[newDeviceID]; exists {
		return IdentityRecord{}, fmt.Errorf("identity %q already exists", newDeviceID)
	}
	now := r.now().UTC()
	old.Status = IdentityReplaced
	old.Reason = "device replaced"
	old.ReplacedBy = newDeviceID
	old.UpdatedAt = now
	newRecord := IdentityRecord{
		DeviceID: newDeviceID, Kind: old.Kind,
		PublicKey:   base64.StdEncoding.EncodeToString(publicKey),
		Fingerprint: IdentityFingerprint(publicKey), Generation: old.Generation + 1,
		Status: IdentityActive, CreatedAt: now, UpdatedAt: now,
	}
	return newRecord, r.persistLocked(func(next map[string]IdentityRecord) {
		next[oldDeviceID] = old
		next[newDeviceID] = newRecord
	})
}

func (r *IdentityRegistry) Lookup(deviceID string) (IdentityRecord, bool) {
	if r == nil {
		return IdentityRecord{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ensureLoadedLocked() != nil {
		return IdentityRecord{}, false
	}
	record, ok := r.identities[deviceID]
	return record, ok
}

func (r *IdentityRegistry) Verify(deviceID string, message, signature []byte) error {
	return r.VerifyAtGeneration(deviceID, 0, message, signature)
}

// VerifyAtGeneration authenticates only the active public key and, when a
// generation is supplied, requires the caller's expected key generation.
// Generation zero means "current active generation" for compatibility with
// callers that do not carry the registry revision yet.
func (r *IdentityRegistry) VerifyAtGeneration(deviceID string, generation uint64, message, signature []byte) error {
	record, ok := r.Lookup(deviceID)
	if !ok {
		return ErrIdentityNotFound
	}
	if record.Status != IdentityActive {
		return fmt.Errorf("%w: %s", ErrIdentityInactive, record.Status)
	}
	if generation != 0 && record.Generation != generation {
		return fmt.Errorf("%w: got=%d want=%d", ErrIdentityGeneration, record.Generation, generation)
	}
	publicKey, err := base64.StdEncoding.DecodeString(record.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(publicKey), message, signature) {
		return errors.New("invalid identity signature")
	}
	return nil
}

func (r *IdentityRegistry) ensureLoadedLocked() error {
	if r.loaded {
		return nil
	}
	return errors.New("identity registry is not loaded")
}

func (r *IdentityRegistry) persistLocked(update func(map[string]IdentityRecord)) error {
	next := cloneIdentityRecords(r.identities)
	update(next)
	disk, err := json.MarshalIndent(identityRegistryDisk{Version: IdentityRegistryVersion, Identities: next}, "", "  ")
	if err != nil {
		return err
	}
	disk = append(disk, '\n')
	if err := configfile.WriteAtomicWithBackup(r.path, disk, 0600); err != nil {
		return err
	}
	r.identities = next
	return nil
}

func validateIdentityInput(deviceID string, kind IdentityKind, publicKey ed25519.PublicKey) error {
	if !identityIDPattern.MatchString(strings.TrimSpace(deviceID)) {
		return errors.New("invalid identity device id")
	}
	if kind != IdentityCentral && kind != IdentityCamera {
		return errors.New("invalid identity kind")
	}
	return validatePublicKey(publicKey)
}

func validatePublicKey(publicKey ed25519.PublicKey) error {
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid identity public key")
	}
	return nil
}

func validateRecord(id string, record IdentityRecord) error {
	if id != record.DeviceID {
		return errors.New("identity key mismatch")
	}
	if err := validateIdentityInput(record.DeviceID, record.Kind, mustDecodePublicKey(record.PublicKey)); err != nil {
		return err
	}
	if record.Fingerprint != IdentityFingerprint(mustDecodePublicKey(record.PublicKey)) || record.Generation == 0 {
		return errors.New("identity fingerprint or generation invalid")
	}
	switch record.Status {
	case IdentityActive, IdentityRevoked, IdentityReplaced:
		return nil
	default:
		return errors.New("invalid identity status")
	}
}

func mustDecodePublicKey(encoded string) ed25519.PublicKey {
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil
	}
	return ed25519.PublicKey(decoded)
}

func cloneIdentityRecords(source map[string]IdentityRecord) map[string]IdentityRecord {
	clone := make(map[string]IdentityRecord, len(source))
	for id, record := range source {
		clone[id] = record
		if record.RevokedAt != nil {
			at := *record.RevokedAt
			cloned := clone[id]
			cloned.RevokedAt = &at
			clone[id] = cloned
		}
	}
	return clone
}
