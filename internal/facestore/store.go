package facestore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
)

const DefaultRoot = "/var/lib/synora/vision/face"

const (
	DefaultMaxUploadSize = 5 << 20
	DefaultMaxPixels     = 25_000_000
	DefaultPartMaxAge    = 24 * time.Hour
)

type Limits struct {
	MaxUploadSize int64
	MaxPixels     int64
	PartMaxAge    time.Duration
}

type Store struct {
	Root   string
	Limits Limits
	Now    func() time.Time
}

type Received struct {
	Photo contract.FacePhoto
	Path  string
}

func New(root string, limits Limits) *Store {
	root = strings.TrimSpace(root)
	if root == "" {
		root = DefaultRoot
	}
	if limits.MaxUploadSize <= 0 {
		limits.MaxUploadSize = DefaultMaxUploadSize
	}
	if limits.MaxPixels <= 0 {
		limits.MaxPixels = DefaultMaxPixels
	}
	if limits.PartMaxAge <= 0 {
		limits.PartMaxAge = DefaultPartMaxAge
	}
	return &Store{Root: filepath.Clean(root), Limits: limits, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) Init() error {
	if s == nil || !filepath.IsAbs(s.Root) {
		return fmt.Errorf("face root must be absolute")
	}
	for _, dir := range []string{s.Root, s.uploadDir(), s.sourceDir(), s.datasetDir(), s.versionDir(), s.stagingDir(), s.legacyDir()} {
		if err := mkdirNoSymlink(dir, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) uploadDir() string  { return filepath.Join(s.Root, "uploads") }
func (s *Store) sourceDir() string  { return filepath.Join(s.Root, "sources") }
func (s *Store) datasetDir() string { return filepath.Join(s.Root, "datasets") }
func (s *Store) versionDir() string { return filepath.Join(s.datasetDir(), "versions") }
func (s *Store) stagingDir() string { return filepath.Join(s.datasetDir(), "staging") }
func (s *Store) legacyDir() string  { return filepath.Join(s.Root, "legacy") }

func SafeComponent(value string) bool {
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value &&
		!filepath.IsAbs(value) && !strings.ContainsAny(value, `/\\`) && !strings.ContainsRune(value, 0)
}

func mkdirNoSymlink(path string, mode os.FileMode) error {
	clean := filepath.Clean(path)
	if info, err := os.Lstat(clean); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("unsafe face storage directory")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(clean)
	if parent != clean {
		if err := mkdirNoSymlink(parent, mode); err != nil {
			return err
		}
	}
	if err := os.Mkdir(clean, mode); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(clean)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("unsafe face storage directory")
	}
	return nil
}

func (s *Store) SourcePath(residentID, storageKey string) (string, error) {
	if !SafeComponent(residentID) {
		return "", contract.NewAPIError(contract.ErrorValidationFailed, "invalid resident id")
	}
	parts := strings.Split(filepath.ToSlash(storageKey), "/")
	if len(parts) != 2 || parts[0] != residentID || !SafeComponent(parts[1]) {
		return "", contract.NewAPIError(contract.ErrorValidationFailed, "invalid face storage key")
	}
	base := filepath.Join(s.sourceDir(), residentID)
	if err := mkdirNoSymlink(base, 0o750); err != nil {
		return "", err
	}
	path := filepath.Join(base, parts[1])
	if rel, err := filepath.Rel(base, path); err != nil || rel != parts[1] {
		return "", contract.NewAPIError(contract.ErrorValidationFailed, "invalid face storage key")
	}
	return path, nil
}

func (s *Store) Receive(residentID string, reader io.Reader) (Received, error) {
	if err := s.Init(); err != nil {
		return Received{}, err
	}
	if !SafeComponent(residentID) {
		return Received{}, contract.NewAPIError(contract.ErrorValidationFailed, "invalid resident id")
	}
	now := s.Now()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tmp, err := os.OpenFile(filepath.Join(s.uploadDir(), ".part-"+idgen.New("face")), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return Received{}, err
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		_ = tmp.Close()
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()
	hash := sha256.New()
	limited := io.LimitReader(reader, s.Limits.MaxUploadSize+1)
	n, err := io.Copy(tmp, io.TeeReader(limited, hash))
	if err != nil {
		return Received{}, err
	}
	if n > s.Limits.MaxUploadSize {
		return Received{}, contract.NewAPIError(contract.ErrorPayloadTooLarge, "face photo exceeds upload limit")
	}
	if n == 0 {
		return Received{}, contract.NewAPIError(contract.ErrorValidationFailed, "empty face photo")
	}
	if err := tmp.Sync(); err != nil {
		return Received{}, err
	}
	if err := tmp.Close(); err != nil {
		return Received{}, err
	}
	mediaType, width, height, err := validateImage(tmpPath, s.Limits.MaxPixels)
	if err != nil {
		return Received{}, err
	}
	ext := extension(mediaType)
	photoID := idgen.New("face")
	name := photoID + ext
	key := residentID + "/" + name
	destination, err := s.SourcePath(residentID, key)
	if err != nil {
		return Received{}, err
	}
	if err := os.Rename(tmpPath, destination); err != nil {
		return Received{}, err
	}
	keep = true
	syncDir(filepath.Dir(destination))
	checksum := hex.EncodeToString(hash.Sum(nil))
	received := now
	return Received{Path: destination, Photo: contract.FacePhoto{
		ID: photoID, ResidentID: residentID, Filename: name, StorageKey: key,
		CreatedAt: now, ReceivedAt: &received, UpdatedAt: now, Status: string(contract.FacePhotoStored),
		SizeBytes: n, Checksum: checksum, MediaType: mediaType, Width: width, Height: height,
		Source: "resident_upload", Revision: 1,
	}}, nil
}

func validateImage(path string, maxPixels int64) (string, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, 0, err
	}
	defer file.Close()
	header := make([]byte, 512)
	n, readErr := io.ReadFull(file, header)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return "", 0, 0, contract.NewAPIError(contract.ErrorValidationFailed, "invalid image")
	}
	mediaType := http.DetectContentType(header[:n])
	if mediaType != "image/jpeg" && mediaType != "image/png" {
		return "", 0, 0, contract.NewAPIError(contract.ErrorValidationFailed, "unsupported image format")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, err
	}
	config, _, err := image.DecodeConfig(file)
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return "", 0, 0, contract.NewAPIError(contract.ErrorValidationFailed, "image decode failed")
	}
	if int64(config.Width)*int64(config.Height) > maxPixels {
		return "", 0, 0, contract.NewAPIError(contract.ErrorValidationFailed, "image pixel limit exceeded")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", 0, 0, err
	}
	if _, _, err := image.Decode(file); err != nil {
		return "", 0, 0, contract.NewAPIError(contract.ErrorValidationFailed, "image decode failed")
	}
	return mediaType, config.Width, config.Height, nil
}

func extension(mediaType string) string {
	if mediaType == "image/png" {
		return ".png"
	}
	return ".jpg"
}

func (s *Store) CleanupParts(now time.Time) (int, error) {
	if s == nil {
		return 0, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	entries, err := os.ReadDir(s.uploadDir())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".part-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return removed, err
		}
		if now.Sub(info.ModTime().UTC()) >= s.Limits.PartMaxAge {
			if err := os.Remove(filepath.Join(s.uploadDir(), entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

func (s *Store) RemoveSource(photo contract.FacePhoto) error {
	path, err := s.SourcePath(photo.ResidentID, photo.StorageKey)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// OrphanSourceKeys reports source files which have no Core metadata. It never
// deletes or imports them; callers may quarantine them after an explicit
// operator decision.
func (s *Store) OrphanSourceKeys(known map[string]bool) ([]string, error) {
	if err := s.Init(); err != nil {
		return nil, err
	}
	entries := []string{}
	residents, err := os.ReadDir(s.sourceDir())
	if err != nil {
		return nil, err
	}
	for _, resident := range residents {
		if !resident.IsDir() {
			continue
		}
		files, readErr := os.ReadDir(filepath.Join(s.sourceDir(), resident.Name()))
		if readErr != nil {
			return nil, readErr
		}
		for _, file := range files {
			if file.IsDir() || !SafeComponent(file.Name()) {
				continue
			}
			key := resident.Name() + "/" + file.Name()
			info, statErr := file.Info()
			if statErr != nil || !info.Mode().IsRegular() {
				continue
			}
			if !known[key] {
				entries = append(entries, key)
			}
		}
	}
	sort.Strings(entries)
	return entries, nil
}

func syncDir(path string) {
	dir, err := os.Open(path)
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
}
