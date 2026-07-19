// Package configfile provides the durable write primitive used by Core-owned
// configuration. Callers must validate the complete replacement before calling
// WriteAtomicWithBackup.
package configfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WriteAtomicWithBackup creates a timestamped backup of an existing file and
// atomically replaces it. The backup lives in a sibling "backups" directory;
// for /etc/synora/foo.yaml this is /etc/synora/backups/foo.TIMESTAMP.yaml.
func WriteAtomicWithBackup(path string, data []byte, mode fs.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("configuration path is required")
	}
	if mode == 0 {
		mode = 0o640
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}

	if previous, err := os.ReadFile(path); err == nil {
		if _, err := writeBackup(path, previous, mode); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read current configuration: %w", err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary configuration: %w", err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set temporary configuration mode: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temporary configuration: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temporary configuration: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary configuration: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("commit configuration: %w", err)
	}
	committed = true
	syncDir(dir)
	return nil
}

// WriteAtomicNew creates path atomically and refuses to replace an existing
// file. It is intended for first-generation identities and other values whose
// accidental replacement would be a security or continuity failure.
func WriteAtomicNew(path string, data []byte, mode fs.FileMode) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("configuration path is required")
	}
	if mode == 0 {
		mode = 0o600
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	tmp, err := os.OpenFile(filepath.Join(dir, "."+filepath.Base(path)+"-new.tmp"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		// A unique temporary name avoids treating a stale temp as an existing
		// identity, while the destination link below remains no-replace.
		tmp, err = os.CreateTemp(dir, "."+filepath.Base(path)+"-new-*.tmp")
		if err != nil {
			return fmt.Errorf("create atomic file: %w", err)
		}
		if err := tmp.Chmod(mode); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
			return fmt.Errorf("set atomic file mode: %w", err)
		}
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write atomic file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync atomic file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close atomic file: %w", err)
	}
	if err := os.Link(tmpPath, path); err != nil {
		return fmt.Errorf("create file without replacement: %w", err)
	}
	committed = true
	_ = os.Remove(tmpPath)
	syncDir(dir)
	return nil
}

// BackupExisting copies the current file to its timestamped backup directory.
// It returns an empty path when the source does not exist.
func BackupExisting(path string, mode fs.FileMode) (string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read configuration for backup: %w", err)
	}
	return writeBackup(path, data, mode)
}

func writeBackup(path string, data []byte, mode fs.FileMode) (string, error) {
	dir := filepath.Join(filepath.Dir(path), "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	ext := filepath.Ext(path)
	name := strings.TrimSuffix(filepath.Base(path), ext)
	stamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	backupPath := filepath.Join(dir, name+"."+stamp+ext)
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return "", fmt.Errorf("create configuration backup: %w", err)
	}
	complete := false
	defer func() {
		_ = file.Close()
		if !complete {
			_ = os.Remove(backupPath)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", fmt.Errorf("write configuration backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync configuration backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close configuration backup: %w", err)
	}
	complete = true
	syncDir(dir)
	return backupPath, nil
}

func syncDir(path string) {
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}
