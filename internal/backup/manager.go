// Package backup provides local, checksum-verified V1 snapshots.
package backup

import (
	"context"
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
}

type StateSummary struct {
	Clips      int `json:"clips"`
	Incidents  int `json:"incidents"`
	Presence   int `json:"presence"`
	FacePhotos int `json:"face_photos"`
}

type Manager struct {
	Root         string
	MinFreeBytes uint64
	Now          func() time.Time
	BeforeCommit func(string) error
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
	if err := checkReserve(m.Root, uint64(len(stateData)), m.MinFreeBytes); err != nil {
		return Manifest{}, err
	}
	if err := writeSynced(filepath.Join(staging, "state.json"), stateData, 0o600); err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{SchemaVersion: ManifestVersion, ID: id, CreatedAt: now, StateBytes: int64(len(stateData)), StateSHA256: digest(stateData), StateSummary: StateSummary{Clips: len(persisted.Clips), Incidents: len(persisted.Incidents), Presence: len(persisted.Presence), FacePhotos: len(persisted.FacePhotos)}, Metadata: map[string]string{"scope": "local-first"}}
	names := make([]string, 0, len(sourceFiles))
	for name := range sourceFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		if !safeName(name) {
			return Manifest{}, fmt.Errorf("invalid backup file name %q", name)
		}
		if info, err := os.Lstat(sourceFiles[name]); err != nil {
			return Manifest{}, err
		} else if !info.Mode().IsRegular() {
			return Manifest{}, errors.New("backup source must be a regular file")
		} else if err := checkReserve(m.Root, uint64(info.Size()), m.MinFreeBytes); err != nil {
			return Manifest{}, err
		}
		record, err := copyIntoSnapshot(ctx, sourceFiles[name], filepath.Join(staging, "files", name))
		if err != nil {
			return Manifest{}, err
		}
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
	stateData, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil || int64(len(stateData)) != manifest.StateBytes || digest(stateData) != manifest.StateSHA256 {
		return errors.New("backup state checksum mismatch")
	}
	var persisted state.PersistedState
	if err := json.Unmarshal(stateData, &persisted); err != nil {
		return fmt.Errorf("decode backup state: %w", err)
	}
	for _, file := range manifest.Files {
		if err := verifyFile(filepath.Join(root, "files", file.Name), file); err != nil {
			return err
		}
	}
	if err := store.RestorePersistedState(&persisted); err != nil {
		return err
	}
	for _, file := range manifest.Files {
		destination, ok := destinations[file.Name]
		if !ok {
			continue
		}
		if err := restoreFile(filepath.Join(root, "files", file.Name), destination); err != nil {
			return err
		}
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
	return manifest, nil
}

func copyIntoSnapshot(ctx context.Context, source, destination string) (FileRecord, error) {
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() {
		return FileRecord{}, errors.New("backup source must be a regular file")
	}
	in, err := os.Open(source)
	if err != nil {
		return FileRecord{}, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return FileRecord{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".copy-")
	if err != nil {
		return FileRecord{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), in)
	if err == nil {
		err = ctx.Err()
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

func restoreFile(source, destination string) error {
	if !filepath.IsAbs(destination) {
		return errors.New("backup destination must be absolute")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	return writeSynced(destination, data, 0o640)
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
