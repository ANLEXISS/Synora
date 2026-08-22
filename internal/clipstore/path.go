package clipstore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func SafeComponent(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.') {
			return false
		}
	}
	return true
}

func FinalPath(root, cameraID, clipID string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("clip root is required")
	}
	if !SafeComponent(cameraID) || !SafeComponent(clipID) {
		return "", fmt.Errorf("unsafe clip path component")
	}
	return filepath.Join(root, cameraID, clipID+".mp4"), nil
}

func PartPath(root, cameraID, clipID string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" || !SafeComponent(cameraID) || !SafeComponent(clipID) {
		return "", fmt.Errorf("unsafe clip part path")
	}
	return filepath.Join(root, cameraID, "."+clipID+".part"), nil
}

// EnsureCameraDir creates the configured spool directories without accepting
// a symlink at the storage root or camera boundary. The root is deployment
// configuration; cameraID and clipID remain server-validated components.
func EnsureCameraDir(root, cameraID string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" || !SafeComponent(cameraID) {
		return "", fmt.Errorf("unsafe clip storage directory")
	}
	if err := ensureDirectory(root, 0700); err != nil {
		return "", err
	}
	cameraDir := filepath.Join(root, cameraID)
	if err := ensureDirectory(cameraDir, 0700); err != nil {
		return "", err
	}
	return cameraDir, nil
}

func ensureDirectory(path string, mode os.FileMode) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("clip storage path is not a directory")
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("clip storage path is not a directory")
	}
	return os.Chmod(path, mode)
}

// VerifyRegularFile checks the final object without following a symlink and,
// when supplied, verifies the bounded on-disk size and streaming SHA-256.
func VerifyRegularFile(path string, expectedSize int64, expectedChecksum string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, nil
	}
	if expectedSize >= 0 && info.Size() != expectedSize {
		return false, nil
	}
	if strings.TrimSpace(expectedChecksum) == "" {
		return true, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), strings.TrimSpace(expectedChecksum)), nil
}

// ReconcileOrphans removes old finalized clip files that have no durable
// metadata reference. A file younger than maxAge is retained so a crash
// between rename and publication can still be recovered by the next state
// load. Symlinks and non-MP4 files are never touched.
func ReconcileOrphans(root string, referenced map[string]struct{}, now time.Time, maxAge time.Duration) (int, error) {
	root = strings.TrimSpace(root)
	if root == "" || maxAge <= 0 {
		return 0, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("clip storage root is not a directory")
	}
	removed := 0
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".mp4") {
			return nil
		}
		if _, ok := referenced[filepath.Clean(path)]; ok {
			return nil
		}
		stat, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if now.Sub(stat.ModTime().UTC()) < maxAge {
			return nil
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		removed++
		return nil
	})
	return removed, err
}
