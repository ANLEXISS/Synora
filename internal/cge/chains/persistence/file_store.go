package persistence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"synora/internal/cge/chains"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/contractcatalog"
)

const defaultFileMode fs.FileMode = 0o640

// FileStoreOptions configures a local snapshot store. Parent directory
// creation is opt-in for the options constructor; NewFileStore enables it
// explicitly for the ordinary local-file use case.
type FileStoreOptions struct {
	Mode             fs.FileMode
	MaxSize          int64
	CreateParentDirs bool
}

// FileStore writes and reads one complete CGE registry snapshot. It has no
// background activity and is not connected to Registry mutations.
type FileStore struct {
	Path             string
	Mode             fs.FileMode
	MaxSize          int64
	CreateParentDirs bool
}

// NewFileStore constructs a store with mode 0640, a 64 MiB limit, and explicit
// permission to create the snapshot's parent directory on Save.
func NewFileStore(path string) (*FileStore, error) {
	return NewFileStoreWithOptions(path, FileStoreOptions{CreateParentDirs: true})
}

// NewFileStoreWithOptions constructs a store with caller-selected safe local
// file options.
func NewFileStoreWithOptions(path string, options FileStoreOptions) (*FileStore, error) {
	store := &FileStore{
		Path:             path,
		Mode:             options.Mode,
		MaxSize:          options.MaxSize,
		CreateParentDirs: options.CreateParentDirs,
	}
	if store.Mode == 0 {
		store.Mode = defaultFileMode
	}
	if store.MaxSize == 0 {
		store.MaxSize = DefaultMaxSnapshotSize
	}
	if err := store.validate(); err != nil {
		return nil, err
	}
	return store, nil
}

// Save captures a coherent defensive registry view, validates every chain,
// and atomically replaces the configured snapshot path. createdAt is required
// explicitly so persistence never chooses an implicit wall clock.
func (s *FileStore) Save(ctx context.Context, source *registry.Registry, createdAt time.Time) (SnapshotMetadata, error) {
	if err := checkContext(ctx); err != nil {
		return SnapshotMetadata{}, err
	}
	if err := s.validate(); err != nil {
		return SnapshotMetadata{}, err
	}
	if source == nil {
		return SnapshotMetadata{}, fmt.Errorf("%w: source registry is nil", ErrInvalidPayload)
	}
	if createdAt.IsZero() {
		return SnapshotMetadata{}, fmt.Errorf("%w: created_at must not be zero", ErrInvalidEnvelope)
	}

	snapshots := source.List()
	if err := checkContext(ctx); err != nil {
		return SnapshotMetadata{}, err
	}
	if err := validateSnapshots(ctx, snapshots); err != nil {
		return SnapshotMetadata{}, err
	}
	payload := RegistryPayload{ChainCount: len(snapshots), Chains: snapshots}
	if err := contractcatalog.ValidateStoreWrite("synora.store.cge-generations", "synora.cge.audit-record.v1", payload); err != nil {
		return SnapshotMetadata{}, fmt.Errorf("%w: contract guard: %v", ErrInvalidPayload, err)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return SnapshotMetadata{}, fmt.Errorf("%w: encode payload: %v", ErrInvalidPayload, err)
	}
	checksum := payloadChecksum(payloadBytes)
	envelope := FileEnvelope{
		SchemaVersion: CurrentSchemaVersion,
		CreatedAt:     createdAt,
		Payload:       payloadBytes,
		PayloadSHA256: checksum,
	}
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		return SnapshotMetadata{}, fmt.Errorf("%w: encode envelope: %v", ErrInvalidEnvelope, err)
	}
	if int64(len(envelopeBytes)) > s.maxSize() {
		return SnapshotMetadata{}, fmt.Errorf("%w: size=%d limit=%d", ErrSnapshotTooLarge, len(envelopeBytes), s.maxSize())
	}
	if err := checkContext(ctx); err != nil {
		return SnapshotMetadata{}, err
	}
	if err := s.atomicWrite(ctx, envelopeBytes); err != nil {
		return SnapshotMetadata{}, err
	}
	return SnapshotMetadata{
		SchemaVersion: CurrentSchemaVersion,
		CreatedAt:     createdAt,
		ChainCount:    len(snapshots),
		PayloadSHA256: checksum,
		SizeBytes:     int64(len(envelopeBytes)),
	}, nil
}

// Load verifies and restores a complete new registry. No partial registry is
// returned when any envelope, payload, or chain is invalid.
func (s *FileStore) Load(ctx context.Context) (*registry.Registry, SnapshotMetadata, error) {
	if err := checkContext(ctx); err != nil {
		return nil, SnapshotMetadata{}, err
	}
	if err := s.validate(); err != nil {
		return nil, SnapshotMetadata{}, err
	}
	file, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: %s", ErrSnapshotNotFound, s.Path)
	}
	if err != nil {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: open snapshot: %v", ErrInvalidEnvelope, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(file, s.maxSize()+1))
	closeErr := file.Close()
	if readErr != nil {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: read snapshot: %v", ErrInvalidEnvelope, readErr)
	}
	if closeErr != nil {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: close snapshot: %v", ErrInvalidEnvelope, closeErr)
	}
	if err := checkContext(ctx); err != nil {
		return nil, SnapshotMetadata{}, err
	}
	if len(data) == 0 {
		return nil, SnapshotMetadata{}, ErrSnapshotEmpty
	}
	if int64(len(data)) > s.maxSize() {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: size=%d limit=%d", ErrSnapshotTooLarge, len(data), s.maxSize())
	}

	var envelope FileEnvelope
	if err := decodeSingleJSON(data, &envelope); err != nil {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}
	if envelope.SchemaVersion != CurrentSchemaVersion {
		return nil, SnapshotMetadata{}, UnsupportedSchemaError{Found: envelope.SchemaVersion, Supported: CurrentSchemaVersion}
	}
	if envelope.CreatedAt.IsZero() || len(bytes.TrimSpace(envelope.Payload)) == 0 {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: required envelope field is missing", ErrInvalidEnvelope)
	}
	expected := payloadChecksum(envelope.Payload)
	if envelope.PayloadSHA256 != expected {
		return nil, SnapshotMetadata{}, ChecksumMismatchError{Expected: expected, Found: envelope.PayloadSHA256}
	}
	if err := checkContext(ctx); err != nil {
		return nil, SnapshotMetadata{}, err
	}

	var payload RegistryPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: decode registry payload: %v", ErrInvalidPayload, err)
	}
	if err := checkContext(ctx); err != nil {
		return nil, SnapshotMetadata{}, err
	}
	if payload.Chains == nil {
		return nil, SnapshotMetadata{}, fmt.Errorf("%w: chains list is required", ErrInvalidPayload)
	}
	if payload.ChainCount < 0 || payload.ChainCount != len(payload.Chains) {
		return nil, SnapshotMetadata{}, ChainCountMismatchError{Declared: payload.ChainCount, Actual: len(payload.Chains)}
	}

	snapshots := append([]chains.Snapshot(nil), payload.Chains...)
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].ID < snapshots[j].ID })
	if err := validateSnapshots(ctx, snapshots); err != nil {
		return nil, SnapshotMetadata{}, err
	}
	restored := registry.New()
	for _, snapshot := range snapshots {
		if err := checkContext(ctx); err != nil {
			return nil, SnapshotMetadata{}, err
		}
		chain, err := chains.Restore(snapshot)
		if err != nil {
			return nil, SnapshotMetadata{}, fmt.Errorf("%w: %v", ErrChainRestoreFailed, err)
		}
		if err := restored.Add(chain); err != nil {
			return nil, SnapshotMetadata{}, fmt.Errorf("%w: %v", ErrChainRestoreFailed, err)
		}
	}
	if err := checkContext(ctx); err != nil {
		return nil, SnapshotMetadata{}, err
	}
	return restored, SnapshotMetadata{
		SchemaVersion: envelope.SchemaVersion,
		CreatedAt:     envelope.CreatedAt,
		ChainCount:    payload.ChainCount,
		PayloadSHA256: envelope.PayloadSHA256,
		SizeBytes:     int64(len(data)),
	}, nil
}

func (s *FileStore) validate() error {
	if s == nil {
		return fmt.Errorf("%w: store is nil", ErrInvalidPath)
	}
	path := strings.TrimSpace(s.Path)
	if path == "" {
		return fmt.Errorf("%w: path must not be empty", ErrInvalidPath)
	}
	if path != s.Path {
		return fmt.Errorf("%w: path must not have surrounding whitespace", ErrInvalidPath)
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == string(filepath.Separator) || filepath.Base(clean) == ".." {
		return fmt.Errorf("%w: path must name a file", ErrInvalidPath)
	}
	if s.mode().Type() != 0 || s.mode().Perm() == 0 || s.mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%w: mode must be a regular non-world-writable file mode", ErrInvalidFileMode)
	}
	if s.maxSize() <= 0 {
		return fmt.Errorf("%w: maximum snapshot size must be positive", ErrInvalidSnapshotLimit)
	}
	return nil
}

func (s *FileStore) mode() fs.FileMode {
	if s == nil || s.Mode == 0 {
		return defaultFileMode
	}
	return s.Mode
}

func (s *FileStore) maxSize() int64 {
	if s == nil || s.MaxSize == 0 {
		return DefaultMaxSnapshotSize
	}
	return s.MaxSize
}

func (s *FileStore) atomicWrite(ctx context.Context, data []byte) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	dir := filepath.Dir(s.Path)
	if s.CreateParentDirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("%w: create parent directory: %v", ErrAtomicWriteFailed, err)
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(s.Path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("%w: create temporary file: %v", ErrAtomicWriteFailed, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(s.mode().Perm()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: set temporary mode: %v", ErrAtomicWriteFailed, err)
	}
	if err := writeAll(tmp, data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: write temporary file: %v", ErrAtomicWriteFailed, err)
	}
	if err := checkContext(ctx); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("%w: sync temporary file: %v", ErrAtomicWriteFailed, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("%w: close temporary file: %v", ErrAtomicWriteFailed, err)
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.Path); err != nil {
		return fmt.Errorf("%w: replace snapshot: %v", ErrAtomicWriteFailed, err)
	}
	committed = true
	syncDirectory(dir)
	return nil
}

func validateSnapshots(ctx context.Context, snapshots []chains.Snapshot) error {
	previous := chains.ChainID("")
	for index, snapshot := range snapshots {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if index > 0 && snapshot.ID == previous {
			return fmt.Errorf("%w: %s", ErrDuplicateChainID, snapshot.ID)
		}
		if index > 0 && snapshot.ID < previous {
			return fmt.Errorf("%w: snapshots are not sorted by chain id", ErrInvalidPayload)
		}
		if _, err := chains.Restore(snapshot); err != nil {
			return fmt.Errorf("%w: %v", ErrChainRestoreFailed, err)
		}
		previous = snapshot.ID
	}
	return nil
}

func payloadChecksum(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func syncDirectory(path string) {
	directory, err := os.Open(path)
	if err != nil {
		return
	}
	defer directory.Close()
	_ = directory.Sync()
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidContext, err)
	}
	return nil
}
