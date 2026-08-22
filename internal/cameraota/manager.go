// Package cameraota defines the signed, resumable OTA boundary for Zero 3W
// camera agents. The transport is injected so offline and rollback behavior
// can be qualified without hardware.
package cameraota

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ManifestSchemaVersion = 1

type Manifest struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Model         string `json:"model"`
	MinBootloader string `json:"min_bootloader"`
	Bytes         int64  `json:"bytes"`
	SHA256        string `json:"sha256"`
	Signature     []byte `json:"signature"`
}

func (m Manifest) signingBytes() ([]byte, error) {
	copy := m
	copy.Signature = nil
	return json.Marshal(copy)
}

func (m *Manifest) Sign(key ed25519.PrivateKey) error {
	if m == nil || len(key) != ed25519.PrivateKeySize {
		return errors.New("camera OTA signing key is invalid")
	}
	data, err := m.signingBytes()
	if err != nil {
		return err
	}
	m.Signature = ed25519.Sign(key, data)
	return nil
}

func VerifyManifest(bundle string, manifest Manifest, key ed25519.PublicKey, model, bootloader string) error {
	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.Version == "" || manifest.Model == "" || len(key) != ed25519.PublicKeySize || len(manifest.Signature) != ed25519.SignatureSize {
		return errors.New("invalid camera OTA manifest")
	}
	info, err := os.Lstat(bundle)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != manifest.Bytes {
		return errors.New("camera OTA bundle size mismatch")
	}
	file, err := os.Open(bundle)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != manifest.SHA256 {
		return errors.New("camera OTA checksum mismatch")
	}
	signing, err := manifest.signingBytes()
	if err != nil || !ed25519.Verify(key, signing, manifest.Signature) {
		return errors.New("camera OTA signature verification failed")
	}
	if manifest.Model != model {
		return fmt.Errorf("camera OTA model mismatch: %s", model)
	}
	if manifest.MinBootloader != "" && compareVersion(bootloader, manifest.MinBootloader) < 0 {
		return fmt.Errorf("camera OTA requires bootloader %s", manifest.MinBootloader)
	}
	return nil
}

type Transport interface {
	Online(context.Context, string) (bool, error)
	Install(context.Context, string, string) error
	Reboot(context.Context, string) error
	Health(context.Context, string) error
	MarkGood(context.Context, string) error
	MarkBad(context.Context, string) error
}

type Phase string

const (
	PhasePending    Phase = "pending"
	PhaseInstalling Phase = "installing"
	PhaseRebooting  Phase = "rebooting"
	PhaseValidating Phase = "validating"
	PhaseGood       Phase = "good"
	PhaseRolledBack Phase = "rolled_back"
)

type Record struct {
	SchemaVersion int       `json:"schema_version"`
	DeviceID      string    `json:"device_id"`
	Bundle        string    `json:"bundle"`
	Version       string    `json:"version"`
	Phase         Phase     `json:"phase"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Result struct {
	DeviceID string `json:"device_id"`
	Version  string `json:"version"`
	Phase    Phase  `json:"phase"`
	Queued   bool   `json:"queued"`
}

type Manager struct {
	Root      string
	PublicKey ed25519.PublicKey
	Transport Transport
	Now       func() time.Time
}

func New(root string, key ed25519.PublicKey, transport Transport) *Manager {
	return &Manager{Root: filepath.Clean(strings.TrimSpace(root)), PublicKey: key, Transport: transport, Now: func() time.Time { return time.Now().UTC() }}
}

func (m *Manager) Apply(ctx context.Context, deviceID, bundle string, manifest Manifest, model, bootloader string) (Result, error) {
	if m == nil || m.Transport == nil || !safeComponent(deviceID) {
		return Result{}, errors.New("camera OTA dependencies unavailable")
	}
	if err := VerifyManifest(bundle, manifest, m.PublicKey, model, bootloader); err != nil {
		return Result{}, err
	}
	now := m.Now()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	record := Record{SchemaVersion: 1, DeviceID: deviceID, Bundle: bundle, Version: manifest.Version, Phase: PhasePending, UpdatedAt: now}
	online, err := m.Transport.Online(ctx, deviceID)
	if err != nil {
		return Result{}, err
	}
	if !online {
		if err := m.save(record); err != nil {
			return Result{}, err
		}
		return Result{DeviceID: deviceID, Version: manifest.Version, Phase: PhasePending, Queued: true}, nil
	}
	record.Phase = PhaseInstalling
	if err := m.save(record); err != nil {
		return Result{}, err
	}
	if err := m.Transport.Install(ctx, deviceID, bundle); err != nil {
		_ = m.Transport.MarkBad(ctx, deviceID)
		record.Phase = PhaseRolledBack
		_ = m.save(record)
		return Result{}, err
	}
	record.Phase = PhaseRebooting
	if err := m.save(record); err != nil {
		return Result{}, err
	}
	if err := m.Transport.Reboot(ctx, deviceID); err != nil {
		_ = m.Transport.MarkBad(ctx, deviceID)
		record.Phase = PhaseRolledBack
		_ = m.save(record)
		return Result{}, err
	}
	record.Phase = PhaseValidating
	if err := m.save(record); err != nil {
		return Result{}, err
	}
	if err := m.Transport.Health(ctx, deviceID); err != nil {
		_ = m.Transport.MarkBad(ctx, deviceID)
		record.Phase = PhaseRolledBack
		_ = m.save(record)
		return Result{}, err
	}
	if err := m.Transport.MarkGood(ctx, deviceID); err != nil {
		_ = m.Transport.MarkBad(ctx, deviceID)
		record.Phase = PhaseRolledBack
		_ = m.save(record)
		return Result{}, err
	}
	record.Phase = PhaseGood
	if err := m.save(record); err != nil {
		return Result{}, err
	}
	return Result{DeviceID: deviceID, Version: manifest.Version, Phase: PhaseGood}, nil
}

func (m *Manager) Recover(ctx context.Context, deviceID string) error {
	record, err := m.load(deviceID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if record.Phase == PhaseGood || record.Phase == PhaseRolledBack || record.Phase == PhasePending {
		return nil
	}
	if err := m.Transport.MarkBad(ctx, deviceID); err != nil {
		return err
	}
	record.Phase = PhaseRolledBack
	record.UpdatedAt = m.Now()
	return m.save(record)
}

func (m *Manager) PrepareRecoveryImage(output, bundle string, manifest Manifest) error {
	if m == nil || !filepath.IsAbs(output) || !safeComponent(manifest.Version) {
		return errors.New("invalid recovery image target")
	}
	if err := VerifyManifest(bundle, manifest, m.PublicKey, manifest.Model, "999.0.0"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(output), ".recovery-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	source, err := os.Open(bundle)
	if err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, source); err != nil {
		_ = source.Close()
		_ = tmp.Close()
		return err
	}
	_ = source.Close()
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, output)
}

func (m *Manager) save(record Record) error {
	if err := os.MkdirAll(filepath.Join(m.Root, "devices"), 0o750); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	path := filepath.Join(m.Root, "devices", record.DeviceID+".json")
	tmp, err := os.CreateTemp(filepath.Dir(path), ".camera-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	return os.Rename(tmpPath, path)
}

func (m *Manager) load(deviceID string) (Record, error) {
	if !safeComponent(deviceID) {
		return Record{}, errors.New("invalid camera id")
	}
	data, err := os.ReadFile(filepath.Join(m.Root, "devices", deviceID+".json"))
	if err != nil {
		return Record{}, err
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil || record.SchemaVersion != 1 {
		return Record{}, errors.New("invalid camera OTA record")
	}
	return record, nil
}

func safeComponent(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !filepath.IsAbs(value) && !strings.ContainsAny(value, "/\\\x00")
}

func compareVersion(left, right string) int {
	parse := func(value string) []int {
		var result []int
		for _, part := range strings.Split(strings.TrimPrefix(value, "v"), ".") {
			var n int
			_, _ = fmt.Sscanf(part, "%d", &n)
			result = append(result, n)
		}
		return result
	}
	a, b := parse(left), parse(right)
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}
