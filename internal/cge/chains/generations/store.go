package generations

import (
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
	"regexp"
	"strings"
	"sync"
	"time"

	"synora/internal/cge/chains/journal"
	"synora/internal/cge/chains/persistence"
	"synora/internal/cge/chains/registry"
	"synora/internal/cge/contractcatalog"
)

const (
	defaultMode            fs.FileMode = 0o640
	defaultMaxSize         int64       = persistence.DefaultMaxSnapshotSize
	generationDigestLength             = 16
)

var generationIDPattern = regexp.MustCompile(`^snapshot-[0-9]{20}-[0-9a-f]{16}$`)

// StoreOptions configures the local generation directory.
type StoreOptions struct {
	FileMode        fs.FileMode
	MaxSnapshotSize int64
}

// Store owns no open file descriptors. Its mutex serializes generation and
// manifest operations within one process; cross-process locking is outside
// this pass.
type Store struct {
	mu sync.Mutex

	rootDir      string
	snapshotsDir string
	manifestPath string
	mode         fs.FileMode
	maxSize      int64
}

// NewStore creates the configured root and snapshots directory explicitly.
func NewStore(rootDir string, options StoreOptions) (*Store, error) {
	if strings.TrimSpace(rootDir) == "" || rootDir != strings.TrimSpace(rootDir) {
		return nil, ErrInvalidGenerationStore
	}
	mode := options.FileMode
	if mode == 0 {
		mode = defaultMode
	}
	if mode.Type() != 0 || mode.Perm() == 0 || mode.Perm()&0o022 != 0 {
		return nil, ErrInvalidGenerationStore
	}
	maxSize := options.MaxSnapshotSize
	if maxSize == 0 {
		maxSize = defaultMaxSize
	}
	if maxSize <= 0 {
		return nil, ErrInvalidGenerationStore
	}
	root, err := filepath.Abs(filepath.Clean(rootDir))
	if err != nil || root == string(filepath.Separator) {
		return nil, ErrInvalidGenerationStore
	}
	s := &Store{
		rootDir: root, snapshotsDir: filepath.Join(root, "snapshots"), manifestPath: filepath.Join(root, "manifest.json"),
		mode: mode, maxSize: maxSize,
	}
	if err := ensureOrCreateDirectory(s.rootDir, 0o750); err != nil {
		return nil, fmt.Errorf("%w: root directory: %v", ErrInvalidGenerationStore, err)
	}
	if err := ensureOrCreateDirectory(s.snapshotsDir, 0o750); err != nil {
		return nil, fmt.Errorf("%w: snapshots directory: %v", ErrInvalidGenerationStore, err)
	}
	return s, nil
}

// CreateGeneration writes one immutable snapshot generation. No checkpoint
// or manifest is written here.
func (s *Store) CreateGeneration(ctx context.Context, source *registry.Registry, journalSequence uint64, journalHeadHash string, createdAt time.Time) (PendingGeneration, error) {
	if err := checkContext(ctx); err != nil {
		return PendingGeneration{}, err
	}
	if err := s.validate(); err != nil {
		return PendingGeneration{}, err
	}
	if source == nil || journalSequence == 0 || !validSHA256(journalHeadHash) || createdAt.IsZero() {
		return PendingGeneration{}, ErrGenerationMetadataMismatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return PendingGeneration{}, err
	}
	stagingName := fmt.Sprintf("snapshot-staging-%020d.json", journalSequence)
	stagingPath := filepath.Join(s.snapshotsDir, stagingName)
	if _, err := os.Lstat(stagingPath); err == nil {
		return PendingGeneration{}, fmt.Errorf("%w: staging file exists", ErrGenerationAlreadyExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return PendingGeneration{}, fmt.Errorf("%w: inspect staging file: %v", ErrGenerationWriteFailed, err)
	}
	keepStaging := true
	defer func() {
		if keepStaging {
			_ = os.Remove(stagingPath)
		}
	}()
	fileStore, err := persistence.NewFileStoreWithOptions(stagingPath, persistence.FileStoreOptions{
		Mode: s.mode, MaxSize: s.maxSize, CreateParentDirs: false,
	})
	if err != nil {
		return PendingGeneration{}, fmt.Errorf("%w: configure snapshot writer: %v", ErrGenerationWriteFailed, err)
	}
	metadata, err := fileStore.Save(ctx, source, createdAt)
	if err != nil {
		return PendingGeneration{}, fmt.Errorf("%w: write snapshot: %v", ErrGenerationWriteFailed, err)
	}
	digest := strings.TrimPrefix(metadata.PayloadSHA256, "sha256:")
	if len(digest) < generationDigestLength {
		return PendingGeneration{}, fmt.Errorf("%w: snapshot checksum is too short", ErrGenerationWriteFailed)
	}
	generationID := fmt.Sprintf("snapshot-%020d-%s", journalSequence, digest[:generationDigestLength])
	relativePath := filepath.ToSlash(filepath.Join("snapshots", generationID+".json"))
	finalPath := filepath.Join(s.rootDir, filepath.FromSlash(relativePath))
	if _, err := os.Lstat(finalPath); err == nil {
		return PendingGeneration{}, fmt.Errorf("%w: %s", ErrGenerationAlreadyExists, generationID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return PendingGeneration{}, fmt.Errorf("%w: inspect generation: %v", ErrGenerationWriteFailed, err)
	}
	if err := checkContext(ctx); err != nil {
		return PendingGeneration{}, err
	}
	if err := os.Link(stagingPath, finalPath); err != nil {
		return PendingGeneration{}, fmt.Errorf("%w: publish immutable snapshot file: %v", ErrGenerationWriteFailed, err)
	}
	if err := syncDirectory(s.snapshotsDir); err != nil {
		return PendingGeneration{}, fmt.Errorf("%w: sync snapshots directory: %v", ErrGenerationWriteFailed, err)
	}
	if err := os.Remove(stagingPath); err != nil {
		return PendingGeneration{}, fmt.Errorf("%w: remove staging file: %v", ErrGenerationWriteFailed, err)
	}
	keepStaging = false
	if err := syncDirectory(s.snapshotsDir); err != nil {
		return PendingGeneration{}, fmt.Errorf("%w: sync snapshots directory: %v", ErrGenerationWriteFailed, err)
	}
	return PendingGeneration{
		GenerationID: generationID, RelativePath: relativePath, Metadata: metadata,
		IncludedJournalSequence: journalSequence, IncludedJournalHeadHash: journalHeadHash,
	}, nil
}

// PublishManifest atomically names a generation only when the supplied
// checkpoint record exactly proves that the generation is durably journaled.
func (s *Store) PublishManifest(ctx context.Context, generation Generation, checkpoint journal.Record, updatedAt time.Time) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := s.validate(); err != nil {
		return err
	}
	if err := generation.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrManifestGenerationMismatch, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := checkContext(ctx); err != nil {
		return err
	}
	if _, _, err := s.loadGenerationLocked(ctx, generation); err != nil {
		return fmt.Errorf("%w: validate generation: %v", ErrManifestGenerationMismatch, err)
	}
	finalized, err := (PendingGeneration{
		GenerationID: generation.GenerationID, RelativePath: generation.SnapshotPath,
		Metadata: persistence.SnapshotMetadata{
			SchemaVersion: generation.SnapshotSchemaVersion, CreatedAt: generation.SnapshotCreatedAt,
			ChainCount: generation.SnapshotChainCount, PayloadSHA256: generation.SnapshotPayloadSHA256,
			SizeBytes: generation.SnapshotSizeBytes,
		}, IncludedJournalSequence: generation.IncludedJournalSequence,
		IncludedJournalHeadHash: generation.IncludedJournalHeadHash,
	}).Finalize(checkpoint)
	if err != nil || finalized.CheckpointRecordSequence != generation.CheckpointRecordSequence || finalized.CheckpointRecordHash != generation.CheckpointRecordHash {
		return fmt.Errorf("%w: checkpoint does not match generation", ErrCheckpointMismatch)
	}
	manifest, err := newManifest(generation, updatedAt)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrManifestWriteFailed, err)
	}
	if err := writeManifestAtomic(ctx, s.manifestPath, manifest, s.mode); err != nil {
		return fmt.Errorf("%w: %v", ErrManifestWriteFailed, err)
	}
	return nil
}

// LoadManifest reads and validates the sole active-generation pointer.
func (s *Store) LoadManifest(ctx context.Context) (Manifest, error) {
	if err := checkContext(ctx); err != nil {
		return Manifest{}, err
	}
	if err := s.validate(); err != nil {
		return Manifest{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	manifest, err := s.loadManifestLocked(ctx)
	if err != nil {
		return Manifest{}, err
	}
	if _, _, err := s.loadGenerationLocked(ctx, manifest.Active); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ListGenerations returns deterministic metadata for immutable snapshot files.
// Unknown files such as crash leftovers and staging files are retained but are
// not presented as generations. No cleanup or repair is performed.
func (s *Store) ListGenerations(ctx context.Context) ([]GenerationFile, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.snapshotsDir)
	if err != nil {
		return nil, fmt.Errorf("%w: list snapshots: %v", ErrInvalidGenerationStore, err)
	}
	result := make([]GenerationFile, 0)
	for _, entry := range entries {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		generationID := strings.TrimSuffix(name, ".json")
		if !validGenerationID(generationID) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: generation %s is a symbolic link", ErrInvalidGenerationPath, generationID)
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("%w: inspect generation %s: %v", ErrInvalidGenerationPath, generationID, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: generation %s is not a regular file", ErrInvalidGenerationPath, generationID)
		}
		result = append(result, GenerationFile{
			GenerationID: generationID,
			RelativePath: filepath.ToSlash(filepath.Join("snapshots", name)),
			SizeBytes:    info.Size(),
		})
	}
	return result, nil
}

// LoadGeneration loads and validates one immutable generation without
// consulting or changing the active manifest.
func (s *Store) LoadGeneration(ctx context.Context, generation Generation) (*registry.Registry, persistence.SnapshotMetadata, error) {
	if err := checkContext(ctx); err != nil {
		return nil, persistence.SnapshotMetadata{}, err
	}
	if err := s.validate(); err != nil {
		return nil, persistence.SnapshotMetadata{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadGenerationLocked(ctx, generation)
}

func (s *Store) loadGenerationLocked(ctx context.Context, generation Generation) (*registry.Registry, persistence.SnapshotMetadata, error) {
	if err := generation.Validate(); err != nil {
		return nil, persistence.SnapshotMetadata{}, fmt.Errorf("%w: %v", ErrGenerationMetadataMismatch, err)
	}
	path := filepath.Join(s.rootDir, filepath.FromSlash(generation.SnapshotPath))
	if err := ensureRegularFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, persistence.SnapshotMetadata{}, fmt.Errorf("%w: %s", ErrGenerationNotFound, generation.GenerationID)
		}
		return nil, persistence.SnapshotMetadata{}, fmt.Errorf("%w: %v", ErrGenerationMetadataMismatch, err)
	}
	fileStore, err := persistence.NewFileStoreWithOptions(path, persistence.FileStoreOptions{Mode: s.mode, MaxSize: s.maxSize})
	if err != nil {
		return nil, persistence.SnapshotMetadata{}, fmt.Errorf("%w: %v", ErrGenerationMetadataMismatch, err)
	}
	loaded, metadata, err := fileStore.Load(ctx)
	if err != nil {
		return nil, persistence.SnapshotMetadata{}, fmt.Errorf("%w: load snapshot: %v", ErrGenerationMetadataMismatch, err)
	}
	if metadata.SchemaVersion != generation.SnapshotSchemaVersion || !metadata.CreatedAt.Equal(generation.SnapshotCreatedAt) || metadata.ChainCount != generation.SnapshotChainCount || metadata.PayloadSHA256 != generation.SnapshotPayloadSHA256 || metadata.SizeBytes != generation.SnapshotSizeBytes {
		return nil, persistence.SnapshotMetadata{}, ErrGenerationMetadataMismatch
	}
	return loaded, metadata, nil
}

func (s *Store) loadManifestLocked(ctx context.Context) (Manifest, error) {
	if err := ensureRegularFile(s.manifestPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Manifest{}, ErrManifestNotFound
		}
		return Manifest{}, fmt.Errorf("%w: manifest path: %v", ErrManifestInvalid, err)
	}
	file, err := os.Open(s.manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, ErrManifestNotFound
	}
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: open manifest: %v", ErrManifestInvalid, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, s.maxSize+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: read manifest: %v", ErrManifestInvalid, err)
	}
	if int64(len(data)) > s.maxSize || len(data) == 0 {
		return Manifest{}, ErrManifestInvalid
	}
	var manifest Manifest
	if err := decodeSingleJSON(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrManifestInvalid, err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (s *Store) validate() error {
	if s == nil || s.rootDir == "" || s.snapshotsDir != filepath.Join(s.rootDir, "snapshots") || s.manifestPath != filepath.Join(s.rootDir, "manifest.json") || s.mode.Type() != 0 || s.mode.Perm() == 0 || s.mode.Perm()&0o022 != 0 || s.maxSize <= 0 {
		return ErrInvalidGenerationStore
	}
	return nil
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidContext, err)
	}
	return nil
}

func validGenerationID(value string) bool {
	return generationIDPattern.MatchString(value)
}

func validSHA256(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(value[len("sha256:"):])
	return err == nil && value == strings.ToLower(value)
}

func checksumJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func writeManifestAtomic(ctx context.Context, path string, manifest Manifest, mode fs.FileMode) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := contractcatalog.ValidateStoreWrite("synora.store.cge-generations", "synora.cge.generation-manifest.v1", manifest); err != nil {
		return fmt.Errorf("contract guard: %w", err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(mode.Perm()); err != nil {
		_ = file.Close()
		return err
	}
	if err := writeAll(file, data); err != nil {
		_ = file.Close()
		return err
	}
	if err := checkContext(ctx); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := checkContext(ctx); err != nil {
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		return err
	}
	cleanup = false
	return syncDirectory(filepath.Dir(path))
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	err = directory.Sync()
	closeErr := directory.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func ensureRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return ErrInvalidGenerationPath
	}
	return nil
}

func ensureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return ErrInvalidGenerationPath
	}
	if info.Mode().Perm()&0o022 != 0 {
		return ErrInvalidGenerationPath
	}
	return nil
}

func ensureOrCreateDirectory(path string, mode fs.FileMode) error {
	if err := ensureDirectory(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, mode.Perm()); err != nil {
		return err
	}
	return ensureDirectory(path)
}

func decodeSingleJSON(data []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
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
