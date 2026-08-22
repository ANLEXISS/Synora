package facedataset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"synora/internal/facestore"
	"synora/pkg/contract"
)

const ManifestSchemaVersion = 1

type Entry struct {
	ResidentID string    `json:"resident_id"`
	PhotoID    string    `json:"photo_id"`
	StorageKey string    `json:"storage_key"`
	Checksum   string    `json:"checksum"`
	SizeBytes  int64     `json:"size_bytes"`
	MediaType  string    `json:"media_type"`
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Embedding  []float32 `json:"embedding"`
}

type Manifest struct {
	SchemaVersion      int       `json:"schema_version"`
	Version            string    `json:"version"`
	DesiredRevision    uint64    `json:"desired_revision"`
	BuiltAt            time.Time `json:"built_at"`
	ModelFingerprint   string    `json:"model_fingerprint"`
	EmbeddingDimension int       `json:"embedding_dimension"`
	Entries            []Entry   `json:"entries"`
	Checksum           string    `json:"checksum"`
}

type Embedder interface {
	Embed(context.Context, string, contract.FacePhoto) ([]float32, string, error)
}

type ReloadResult struct {
	Version            string
	ActiveRevision     uint64
	EmbeddingDimension int
	ModelFingerprint   string
}

type ValidationError struct {
	PhotoID string
	Code    string
	Err     error
}

func (e *ValidationError) Error() string {
	if e == nil || e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}
func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type Loader interface {
	ReloadFaceDataset(context.Context, string, string) (ReloadResult, error)
}

type Builder struct {
	Store *facestore.Store
	Now   func() time.Time
}

func NewBuilder(store *facestore.Store) *Builder {
	return &Builder{Store: store, Now: func() time.Time { return time.Now().UTC() }}
}

func (b *Builder) BuildAndActivate(ctx context.Context, photos []contract.FacePhoto, desiredRevision uint64, embedder Embedder, loader Loader) (Manifest, error) {
	if b == nil || b.Store == nil || embedder == nil || loader == nil {
		return Manifest{}, errors.New("face dataset dependencies unavailable")
	}
	if err := b.Store.Init(); err != nil {
		return Manifest{}, err
	}
	now := b.Now()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	version := "v-" + strconv.FormatUint(desiredRevision, 10)
	staging, err := os.MkdirTemp(filepath.Join(b.Store.Root, "datasets", "staging"), ".build-")
	if err != nil {
		return Manifest{}, err
	}
	defer os.RemoveAll(staging)
	manifest, err := b.build(ctx, staging, version, now, photos, desiredRevision, embedder)
	if err != nil {
		return Manifest{}, err
	}
	final := filepath.Join(b.Store.Root, "datasets", "versions", version)
	if _, statErr := os.Lstat(final); statErr == nil {
		old, readErr := ReadManifest(final)
		if readErr == nil && old.Checksum == manifest.Checksum {
			return b.reloadAndPoint(ctx, final, manifest, loader)
		}
		return Manifest{}, fmt.Errorf("dataset version collision: %s", version)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Manifest{}, statErr
	}
	if err := os.Rename(staging, final); err != nil {
		return Manifest{}, err
	}
	syncDir(filepath.Dir(final))
	return b.reloadAndPoint(ctx, final, manifest, loader)
}

func (b *Builder) build(ctx context.Context, staging, version string, now time.Time, photos []contract.FacePhoto, desiredRevision uint64, embedder Embedder) (Manifest, error) {
	items := append([]contract.FacePhoto(nil), photos...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].ResidentID == items[j].ResidentID {
			return items[i].ID < items[j].ID
		}
		return items[i].ResidentID < items[j].ResidentID
	})
	manifest := Manifest{SchemaVersion: ManifestSchemaVersion, Version: version, DesiredRevision: desiredRevision, BuiltAt: now, Entries: []Entry{}}
	for _, photo := range items {
		if photo.Status == string(contract.FacePhotoRemoved) || photo.Status == string(contract.FacePhotoRejected) || photo.Status == string(contract.FacePhotoRemovalPending) || photo.Status == string(contract.FacePhotoMissing) {
			continue
		}
		if strings.TrimSpace(photo.ResidentID) == "" || strings.TrimSpace(photo.ID) == "" {
			return Manifest{}, errors.New("face dataset photo identity is incomplete")
		}
		if err := ctx.Err(); err != nil {
			return Manifest{}, err
		}
		source, err := b.Store.SourcePath(photo.ResidentID, photo.StorageKey)
		if err != nil {
			return Manifest{}, err
		}
		if err := verifySource(source, photo.SizeBytes, photo.Checksum); err != nil {
			return Manifest{}, fmt.Errorf("photo %s: %w", photo.ID, err)
		}
		embedding, fingerprint, err := embedder.Embed(ctx, source, photo)
		if err != nil {
			return Manifest{}, &ValidationError{PhotoID: photo.ID, Code: "embedding_failed", Err: err}
		}
		if err := validateEmbedding(embedding); err != nil {
			return Manifest{}, &ValidationError{PhotoID: photo.ID, Code: "embedding_invalid", Err: err}
		}
		if manifest.EmbeddingDimension == 0 {
			manifest.EmbeddingDimension = len(embedding)
		} else if len(embedding) != manifest.EmbeddingDimension {
			return Manifest{}, errors.New("embedding dimension mismatch")
		}
		if manifest.ModelFingerprint == "" {
			manifest.ModelFingerprint = fingerprint
		} else if fingerprint != "" && manifest.ModelFingerprint != fingerprint {
			return Manifest{}, errors.New("model fingerprint mismatch")
		}
		entry := Entry{ResidentID: photo.ResidentID, PhotoID: photo.ID, StorageKey: photo.StorageKey, Checksum: photo.Checksum, SizeBytes: photo.SizeBytes, MediaType: photo.MediaType, Width: photo.Width, Height: photo.Height, Embedding: append([]float32(nil), embedding...)}
		manifest.Entries = append(manifest.Entries, entry)
		if err := copySource(source, filepath.Join(staging, "sources", photo.ResidentID, photo.ID+filepath.Ext(photo.Filename)), photo.SizeBytes, photo.Checksum); err != nil {
			return Manifest{}, err
		}
	}
	manifest.Checksum = manifestChecksum(manifest)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), append(data, '\n'), 0o640); err != nil {
		return Manifest{}, err
	}
	syncDir(staging)
	return manifest, nil
}

func (b *Builder) reloadAndPoint(ctx context.Context, final string, manifest Manifest, loader Loader) (Manifest, error) {
	result, err := loader.ReloadFaceDataset(ctx, manifest.Version, final)
	if err != nil || result.Version != manifest.Version {
		if err == nil {
			err = errors.New("vision acknowledged a different dataset version")
		}
		return Manifest{}, err
	}
	current := filepath.Join(b.Store.Root, "datasets", "current")
	tmp, err := os.CreateTemp(filepath.Dir(current), ".current-*")
	if err != nil {
		return Manifest{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return Manifest{}, err
	}
	if _, err := io.WriteString(tmp, manifest.Version+"\n"); err != nil {
		_ = tmp.Close()
		return Manifest{}, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return Manifest{}, err
	}
	if err := tmp.Close(); err != nil {
		return Manifest{}, err
	}
	if err := os.Rename(tmpPath, current); err != nil {
		return Manifest{}, err
	}
	syncDir(filepath.Dir(current))
	return manifest, nil
}

func ReadManifest(versionDir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(versionDir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.SchemaVersion != ManifestSchemaVersion || manifest.Version == "" || manifest.Checksum == "" || manifest.Checksum != manifestChecksum(manifest) {
		return Manifest{}, errors.New("invalid face dataset manifest")
	}
	if manifest.EmbeddingDimension < 0 {
		return Manifest{}, errors.New("invalid embedding dimension")
	}
	for _, entry := range manifest.Entries {
		if len(entry.Embedding) != manifest.EmbeddingDimension {
			return Manifest{}, errors.New("invalid embedding dimension")
		}
		parts := strings.Split(filepath.ToSlash(entry.StorageKey), "/")
		if len(parts) != 2 || parts[0] != entry.ResidentID || !facestore.SafeComponent(parts[0]) || !facestore.SafeComponent(parts[1]) {
			return Manifest{}, errors.New("invalid face dataset storage key")
		}
		if err := validateEmbedding(entry.Embedding); err != nil {
			return Manifest{}, err
		}
	}
	return manifest, nil
}

func ReadCurrent(root string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "datasets", "current"))
	if err != nil {
		return Manifest{}, err
	}
	version := strings.TrimSpace(string(data))
	if !facestore.SafeComponent(version) {
		return Manifest{}, errors.New("invalid current dataset pointer")
	}
	return ReadManifest(filepath.Join(root, "datasets", "versions", version))
}

// PruneObsolete removes only immutable versions older than keepAge and never
// removes the version named by current. A missing/corrupt current pointer is
// treated as a reason to keep everything for operator reconciliation.
func (b *Builder) PruneObsolete(keepAge time.Duration) (int, error) {
	if b == nil || b.Store == nil || keepAge <= 0 {
		return 0, nil
	}
	return b.pruneObsolete(keepAge, false)
}

func (b *Builder) pruneObsolete(keepAge time.Duration, purgeAll bool) (int, error) {
	if b == nil || b.Store == nil {
		return 0, nil
	}
	current, err := ReadCurrent(b.Store.Root)
	if err != nil {
		return 0, nil
	}
	entries, err := os.ReadDir(filepath.Join(b.Store.Root, "datasets", "versions"))
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == current.Version || !facestore.SafeComponent(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, err
		}
		if !purgeAll && now.Sub(info.ModTime().UTC()) < keepAge {
			continue
		}
		path := filepath.Join(b.Store.Root, "datasets", "versions", entry.Name())
		if linkInfo, err := os.Lstat(path); err != nil || linkInfo.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if err := os.RemoveAll(path); err != nil {
			return removed, err
		}
		removed++
	}
	if removed > 0 {
		syncDir(filepath.Join(b.Store.Root, "datasets", "versions"))
	}
	return removed, nil
}

// PurgeObsolete removes every immutable version except datasets/current. It
// is reserved for explicit biometric deletion; normal retention continues to
// use PruneObsolete with its configured grace period.
func (b *Builder) PurgeObsolete() (int, error) {
	return b.pruneObsolete(0, true)
}

func verifySource(path string, size int64, checksum string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("source is not a regular file")
	}
	if size >= 0 && info.Size() != size {
		return errors.New("source size mismatch")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(hash, file)
	if err != nil {
		return err
	}
	if size >= 0 && n != size {
		return errors.New("source size mismatch")
	}
	if checksum != "" && hex.EncodeToString(hash.Sum(nil)) != checksum {
		return errors.New("source checksum mismatch")
	}
	return nil
}

func copySource(source, destination string, size int64, checksum string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".source-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), in)
	if err != nil {
		return err
	}
	if size >= 0 && n != size {
		return errors.New("copied source size mismatch")
	}
	if checksum != "" && hex.EncodeToString(hash.Sum(nil)) != checksum {
		return errors.New("copied source checksum mismatch")
	}
	if err := tmp.Chmod(0o640); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return err
	}
	committed = true
	syncDir(filepath.Dir(destination))
	return nil
}

func validateEmbedding(value []float32) error {
	if len(value) == 0 {
		return errors.New("empty embedding")
	}
	norm := 0.0
	for _, item := range value {
		if math.IsNaN(float64(item)) || math.IsInf(float64(item), 0) {
			return errors.New("non-finite embedding")
		}
		norm += float64(item) * float64(item)
	}
	if norm <= 0 {
		return errors.New("zero embedding")
	}
	return nil
}

func manifestChecksum(manifest Manifest) string {
	manifest.Checksum = ""
	data, _ := json.Marshal(manifest)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func syncDir(path string) {
	dir, err := os.Open(path)
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
}
