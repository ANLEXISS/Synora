package clipstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
