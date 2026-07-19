package fieldtrial

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type retentionSession struct {
	Path     string
	Manifest SessionManifest
	Size     int64
}

func Prune(ctx context.Context, config Config, now time.Time) (int, error) {
	if err := contextErr(ctx); err != nil {
		return 0, err
	}
	if err := config.Validate(); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(config.RootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	values := make([]retentionSession, 0)
	for _, entry := range entries {
		info, statErr := os.Lstat(filepath.Join(config.RootDir, entry.Name()))
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(config.RootDir, entry.Name())
		manifest, err := ReadManifest(path)
		if err != nil || manifest.Status == SessionOpen || manifest.Status == SessionRecovered {
			continue
		}
		var size int64
		_ = filepath.Walk(path, func(child string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.Mode().IsRegular() {
				size += info.Size()
			}
			return nil
		})
		values = append(values, retentionSession{path, manifest, size})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Manifest.CreatedAt.Before(values[j].Manifest.CreatedAt) })
	cutoff := now.AddDate(0, 0, -config.RetentionDays)
	total := int64(0)
	for _, value := range values {
		total += value.Size
	}
	deleted := 0
	for _, value := range values {
		if value.Manifest.ClosedAt == nil {
			continue
		}
		remove := config.RetentionDays > 0 && value.Manifest.ClosedAt.Before(cutoff)
		if !remove && config.MaximumTotalBytes > 0 && total > config.MaximumTotalBytes {
			remove = true
		}
		if !remove {
			continue
		}
		if err := removeSessionDirectory(value.Path); err != nil {
			return deleted, err
		}
		total -= value.Size
		deleted++
	}
	return deleted, nil
}

func removeSessionDirectory(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			if err := os.Remove(child); err != nil {
				return err
			}
			continue
		}
		if err := removeSessionDirectory(child); err != nil {
			return err
		}
	}
	return os.Remove(path)
}
