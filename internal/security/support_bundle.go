package security

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CleanSupportBundle removes biometric artifacts and redacts text before a
// support bundle is handed to an operator. It only accepts an absolute,
// non-symlink directory and never follows links.
func CleanSupportBundle(root string) (int, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "." || !filepath.IsAbs(root) {
		return 0, errors.New("support bundle root must be absolute")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("support bundle root must be a directory")
	}
	removed := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("support bundle contains symlink")
		}
		if entry.IsDir() {
			if sensitiveBundleName(entry.Name()) {
				count, err := removeSupportTree(path)
				removed += count
				if err != nil {
					return err
				}
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(entry.Name())
		if sensitiveBundleName(name) {
			if err := os.Remove(path); err != nil {
				return err
			}
			removed++
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		redacted := []byte(RedactSupportText(string(data)))
		if string(redacted) != string(data) {
			if err := os.WriteFile(path, redacted, 0o600); err != nil {
				return err
			}
		} else if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
		return nil
	})
	return removed, err
}

func removeSupportTree(path string) (int, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			return removed, fmt.Errorf("support bundle contains symlink")
		}
		if entry.IsDir() {
			count, err := removeSupportTree(child)
			removed += count
			if err != nil {
				return removed, err
			}
			continue
		}
		if err := os.Remove(child); err != nil {
			return removed, err
		}
		removed++
	}
	if err := os.Remove(path); err != nil {
		return removed, err
	}
	return removed, nil
}

func sensitiveBundleName(name string) bool {
	if strings.Contains(name, "embedding") || strings.Contains(name, "biometric") || strings.Contains(name, "face") {
		return true
	}
	switch filepath.Ext(name) {
	case ".jpg", ".jpeg", ".png", ".webp", ".npy", ".npz":
		return true
	default:
		return false
	}
}
