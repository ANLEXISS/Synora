// Package backup provides local, checksum-verified V1 snapshots.
package backup

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"synora/internal/state"
)

const ManifestVersion = 1

type FileRecord struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	CreatedAt     time.Time         `json:"created_at"`
	StateBytes    int64             `json:"state_bytes"`
	StateSHA256   string            `json:"state_sha256"`
	StateSummary  StateSummary      `json:"state_summary"`
	Files         []FileRecord      `json:"files,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	Encrypted     bool              `json:"encrypted,omitempty"`
	Encryption    string            `json:"encryption,omitempty"`
}

type StateSummary struct {
	Clips      int `json:"clips"`
	Incidents  int `json:"incidents"`
	Presence   int `json:"presence"`
	FacePhotos int `json:"face_photos"`
}

type Manager struct {
	Root          string
	MinFreeBytes  uint64
	Now           func() time.Time
	BeforeCommit  func(string) error
	BeforeRestore func(string) error
	Secret        string
}

func New(root string, minFreeBytes uint64) *Manager {
	return &Manager{Root: filepath.Clean(strings.TrimSpace(root)), MinFreeBytes: minFreeBytes, Now: func() time.Time { return time.Now().UTC() }}
}

func (m *Manager) Init() error {
	if m == nil || !filepath.IsAbs(m.Root) || m.Root == "." {
		return errors.New("backup root must be absolute")
	}
	for _, path := range []string{m.Root, filepath.Join(m.Root, "snapshots"), filepath.Join(m.Root, "staging")} {
		if err := mkdirPrivate(path); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Create(ctx context.Context, store *state.Store, sourceFiles map[string]string) (Manifest, error) {
	if store == nil {
		return Manifest{}, errors.New("state store is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.Init(); err != nil {
		return Manifest{}, err
	}
	now := m.Now()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id := now.Format("20060102T150405.000000000Z07:00")
	// Colons are valid locally but awkward in portable tooling; retain UTC
	// ordering while using only safe path characters.
	id = strings.NewReplacer(":", "", "+", "p", "-", "m").Replace(id)
	staging, err := os.MkdirTemp(filepath.Join(m.Root, "staging"), ".snapshot-")
	if err != nil {
		return Manifest{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	persisted := store.PersistedState()
	stateData, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	stateData = append(stateData, '\n')
	encrypted := m.encryptionEnabled()
	if encrypted {
		stateData, err = m.seal(stateData, id+":state")
		if err != nil {
			return Manifest{}, err
		}
	}
	if err := checkReserve(m.Root, uint64(len(stateData)), m.MinFreeBytes); err != nil {
		return Manifest{}, err
	}
	if err := writeSynced(filepath.Join(staging, "state.json"), stateData, 0o600); err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{SchemaVersion: ManifestVersion, ID: id, CreatedAt: now, StateBytes: int64(len(stateData)), StateSHA256: digest(stateData), StateSummary: StateSummary{Clips: len(persisted.Clips), Incidents: len(persisted.Incidents), Presence: len(persisted.Presence), FacePhotos: len(persisted.FacePhotos)}, Metadata: map[string]string{"scope": "local-first", "state_version": fmt.Sprintf("%d", persisted.Version)}, Encrypted: encrypted}
	if encrypted {
		manifest.Encryption = "aes-256-gcm"
	}
	names := make([]string, 0, len(sourceFiles))
	for name := range sourceFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		if !safeRelativeName(name) {
			return Manifest{}, fmt.Errorf("invalid backup file name %q", name)
		}
		if info, err := os.Lstat(sourceFiles[name]); err != nil {
			return Manifest{}, err
		} else if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return Manifest{}, errors.New("backup source must be a regular file")
		} else if err := checkReserve(m.Root, uint64(info.Size()), m.MinFreeBytes); err != nil {
			return Manifest{}, err
		}
		record, err := copyIntoSnapshot(ctx, sourceFiles[name], filepath.Join(staging, "files", name), func(data []byte) ([]byte, error) {
			if !encrypted {
				return data, nil
			}
			return m.seal(data, id+":file:"+filepath.ToSlash(name))
		})
		if err != nil {
			return Manifest{}, err
		}
		record.Name = filepath.ToSlash(name)
		manifest.Files = append(manifest.Files, record)
	}
	if m.BeforeCommit != nil {
		if err := m.BeforeCommit("before_manifest"); err != nil {
			return Manifest{}, err
		}
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := writeSynced(filepath.Join(staging, "manifest.json"), append(manifestData, '\n'), 0o600); err != nil {
		return Manifest{}, err
	}
	if err := syncDir(filepath.Join(staging, "files")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	}
	if err := syncDir(staging); err != nil {
		return Manifest{}, err
	}
	final := filepath.Join(m.Root, "snapshots", id)
	if err := os.Rename(staging, final); err != nil {
		return Manifest{}, err
	}
	committed = true
	_ = syncDir(filepath.Join(m.Root, "snapshots"))
	return manifest, nil
}

func (m *Manager) Restore(ctx context.Context, id string, store *state.Store, destinations map[string]string) error {
	if store == nil || !safeName(id) {
		return errors.New("invalid backup restore request")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.Init(); err != nil {
		return err
	}
	root := filepath.Join(m.Root, "snapshots", id)
	manifest, err := readManifest(root)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if manifest.Encrypted {
		if manifest.Encryption != "aes-256-gcm" || !m.encryptionEnabled() {
			return errors.New("backup secret is required")
		}
	}
	stateData, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil || int64(len(stateData)) != manifest.StateBytes || digest(stateData) != manifest.StateSHA256 {
		return errors.New("backup state checksum mismatch")
	}
	if manifest.Encrypted {
		stateData, err = m.open(stateData, manifest.ID+":state")
		if err != nil {
			return errors.New("backup secret is invalid")
		}
	}
	var persisted state.PersistedState
	if err := json.Unmarshal(stateData, &persisted); err != nil {
		return fmt.Errorf("decode backup state: %w", err)
	}
	if err := normalizeBackupState(&persisted); err != nil {
		return err
	}
	names := make([]string, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		if !safeRelativeName(file.Name) {
			return errors.New("backup manifest contains unsafe file name")
		}
		if err := verifyFile(filepath.Join(root, "files", filepath.FromSlash(file.Name)), file); err != nil {
			return err
		}
		if destination, ok := destinations[file.Name]; ok {
			if err := validateDestination(destination); err != nil {
				return err
			}
		}
		names = append(names, file.Name)
	}
	sort.Strings(names)
	oldFiles := make(map[string]fileBackup, len(names))
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			rollbackDestinations(oldFiles, destinations)
			return err
		}
		destination, ok := destinations[name]
		if !ok {
			continue
		}
		backup, err := captureDestination(destination)
		if err != nil {
			return err
		}
		oldFiles[name] = backup
	}
	for _, name := range names {
		destination, ok := destinations[name]
		if !ok {
			continue
		}
		if m.BeforeRestore != nil {
			if err := m.BeforeRestore(name); err != nil {
				rollbackDestinations(oldFiles, destinations)
				return err
			}
		}
		if err := restoreFile(filepath.Join(root, "files", filepath.FromSlash(name)), destination, func(data []byte) ([]byte, error) {
			if !manifest.Encrypted {
				return data, nil
			}
			return m.open(data, manifest.ID+":file:"+name)
		}); err != nil {
			rollbackDestinations(oldFiles, destinations)
			return err
		}
	}
	if err := store.RestorePersistedState(&persisted); err != nil {
		rollbackDestinations(oldFiles, destinations)
		return err
	}
	return nil
}

func (m *Manager) Expire(before time.Time) (int, error) {
	if err := m.Init(); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(filepath.Join(m.Root, "snapshots"))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || !safeName(entry.Name()) {
			continue
		}
		path := filepath.Join(m.Root, "snapshots", entry.Name())
		manifest, err := readManifest(path)
		if err != nil || !manifest.CreatedAt.Before(before) {
			continue
		}
		deleting := path + ".delete"
		if err := os.Rename(path, deleting); err != nil {
			return removed, err
		}
		if err := os.RemoveAll(deleting); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, syncDir(filepath.Join(m.Root, "snapshots"))
}

func (m *Manager) RecoverExpiredDeletes() error {
	if err := m.Init(); err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(m.Root, "snapshots"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".delete") {
			if err := os.RemoveAll(filepath.Join(m.Root, "snapshots", entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func readManifest(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.SchemaVersion != ManifestVersion || !safeName(manifest.ID) {
		return Manifest{}, errors.New("invalid backup manifest")
	}
	seen := make(map[string]struct{}, len(manifest.Files))
	for _, file := range manifest.Files {
		if !safeRelativeName(file.Name) || file.Bytes < 0 || len(file.SHA256) != sha256.Size*2 {
			return Manifest{}, errors.New("invalid backup manifest file record")
		}
		if _, exists := seen[file.Name]; exists {
			return Manifest{}, errors.New("duplicate backup manifest file")
		}
		seen[file.Name] = struct{}{}
	}
	return manifest, nil
}

func copyIntoSnapshot(ctx context.Context, source, destination string, transform func([]byte) ([]byte, error)) (FileRecord, error) {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return FileRecord{}, errors.New("backup source must be a regular file")
	}
	in, err := os.Open(source)
	if err != nil {
		return FileRecord{}, err
	}
	defer in.Close()
	if err := ensureNoSymlinkParents(filepath.Dir(destination)); err != nil {
		return FileRecord{}, err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return FileRecord{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".copy-")
	if err != nil {
		return FileRecord{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	data, err := io.ReadAll(in)
	if err == nil {
		err = ctx.Err()
	}
	if err == nil && transform != nil {
		data, err = transform(data)
	}
	hash := sha256.New()
	n := int64(0)
	if err == nil {
		n, err = io.Copy(io.MultiWriter(tmp, hash), bytes.NewReader(data))
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return FileRecord{}, err
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return FileRecord{}, err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return FileRecord{}, err
	}
	return FileRecord{Name: filepath.Base(destination), Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func verifyFile(path string, record FileRecord) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != record.Bytes {
		return errors.New("backup file metadata mismatch")
	}
	data, err := os.ReadFile(path)
	if err != nil || digest(data) != record.SHA256 {
		return errors.New("backup file checksum mismatch")
	}
	return nil
}

func restoreFile(source, destination string, transform func([]byte) ([]byte, error)) error {
	if err := validateDestination(destination); err != nil {
		return err
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if transform != nil {
		data, err = transform(data)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	return writeSynced(destination, data, 0o600)
}

func (m *Manager) encryptionEnabled() bool {
	return m != nil && strings.TrimSpace(m.Secret) != ""
}

func (m *Manager) cipher() (cipher.AEAD, error) {
	if !m.encryptionEnabled() {
		return nil, errors.New("backup secret is required")
	}
	key := sha256.Sum256([]byte(m.Secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (m *Manager) seal(data []byte, aad string) ([]byte, error) {
	aead, err := m.cipher()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, data, []byte(aad)), nil
}

func (m *Manager) open(data []byte, aad string) ([]byte, error) {
	aead, err := m.cipher()
	if err != nil {
		return nil, err
	}
	if len(data) < aead.NonceSize() {
		return nil, errors.New("encrypted backup payload is truncated")
	}
	nonce, ciphertext := data[:aead.NonceSize()], data[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, []byte(aad))
}

func writeSynced(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".atomic-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
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
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

func checkReserve(root string, required, reserve uint64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return err
	}
	free := uint64(stat.Bavail) * uint64(stat.Bsize)
	if free < required || free-required < reserve {
		return fmt.Errorf("insufficient backup space: free=%d required=%d reserve=%d", free, required, reserve)
	}
	return nil
}

func mkdirPrivate(path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("unsafe backup directory")
	}
	return os.Chmod(path, 0o750)
}

func safeName(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !filepath.IsAbs(value) && !strings.ContainsAny(value, "/\\\x00")
}

func safeRelativeName(value string) bool {
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "\\\x00") {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return filepath.ToSlash(clean) == value
}

type fileBackup struct {
	exists bool
	data   []byte
	mode   os.FileMode
}

func normalizeBackupState(persisted *state.PersistedState) error {
	if persisted == nil {
		return errors.New("backup state is required")
	}
	switch persisted.Version {
	case state.PersistedStateVersion:
		return nil
	case 1:
		persisted.Version = state.PersistedStateVersion
		if persisted.BehaviorOverrides == nil {
			persisted.BehaviorOverrides = map[string]json.RawMessage{}
		}
		return nil
	default:
		return fmt.Errorf("unsupported backup state version %d", persisted.Version)
	}
}

func validateDestination(destination string) error {
	if !filepath.IsAbs(destination) {
		return errors.New("backup destination must be absolute")
	}
	if err := ensureNoSymlinkParents(filepath.Dir(destination)); err != nil {
		return err
	}
	info, err := os.Lstat(destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("backup destination must be a regular non-symlink file")
	}
	return nil
}

func ensureNoSymlinkParents(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	current := string(filepath.Separator)
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("backup path contains unsafe parent")
		}
	}
	return nil
}

func captureDestination(destination string) (fileBackup, error) {
	if err := validateDestination(destination); err != nil {
		return fileBackup{}, err
	}
	data, err := os.ReadFile(destination)
	if errors.Is(err, os.ErrNotExist) {
		return fileBackup{}, nil
	}
	if err != nil {
		return fileBackup{}, err
	}
	info, err := os.Stat(destination)
	if err != nil {
		return fileBackup{}, err
	}
	return fileBackup{exists: true, data: data, mode: info.Mode().Perm()}, nil
}

func rollbackDestinations(backups map[string]fileBackup, destinations map[string]string) {
	names := make([]string, 0, len(backups))
	for name := range backups {
		names = append(names, name)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		destination := destinations[name]
		backup := backups[name]
		if backup.exists {
			_ = writeSynced(destination, backup.data, backup.mode)
		} else {
			_ = os.Remove(destination)
		}
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
