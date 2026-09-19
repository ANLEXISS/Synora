package discovery

// The media cache is a bounded, short-lived hand-off area for Discovery.  It
// is not Store state and is never exposed on the semantic bus.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrMediaCacheTooLarge = errors.New("media cache item exceeds limit")
	ErrMediaCacheFull     = errors.New("media cache quota exhausted")
	ErrMediaCacheToken    = errors.New("invalid media cache token")
	ErrMediaCacheMissing  = errors.New("media cache item missing")
)

type MediaCacheConfig struct {
	Root       string
	TTL        time.Duration
	MaxBytes   int64
	MaxEntries int
}

type MediaCacheItem struct {
	Token     string
	Path      string
	Size      int64
	SHA256    string
	ExpiresAt time.Time
}

type MediaCache struct {
	mu  sync.Mutex
	cfg MediaCacheConfig
}

func NewMediaCache(cfg MediaCacheConfig) (*MediaCache, error) {
	if strings.TrimSpace(cfg.Root) == "" || cfg.TTL <= 0 || cfg.MaxBytes <= 0 || cfg.MaxEntries <= 0 {
		return nil, errors.New("media cache requires root, positive ttl, quota and entry limit")
	}
	if err := os.MkdirAll(cfg.Root, 0700); err != nil {
		return nil, fmt.Errorf("create media cache: %w", err)
	}
	return &MediaCache{cfg: cfg}, nil
}

func (c *MediaCache) Put(src io.Reader, size int64, now time.Time) (MediaCacheItem, error) {
	if size < 0 || size > c.cfg.MaxBytes {
		return MediaCacheItem{}, ErrMediaCacheTooLarge
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.cleanupLocked(now); err != nil {
		return MediaCacheItem{}, err
	}
	used, entries, err := c.usageLocked()
	if err != nil {
		return MediaCacheItem{}, err
	}
	if entries >= c.cfg.MaxEntries || used > c.cfg.MaxBytes-size {
		return MediaCacheItem{}, ErrMediaCacheFull
	}
	token, err := opaqueToken()
	if err != nil {
		return MediaCacheItem{}, err
	}
	tmp, err := os.CreateTemp(c.cfg.Root, ".pending-")
	if err != nil {
		return MediaCacheItem{}, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(src, size+1))
	if closeErr := tmp.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return MediaCacheItem{}, copyErr
	}
	if written != size {
		return MediaCacheItem{}, ErrMediaCacheTooLarge
	}
	path := filepath.Join(c.cfg.Root, token+".media")
	if err := os.Rename(tmpPath, path); err != nil {
		return MediaCacheItem{}, err
	}
	if err := os.Chtimes(path, now, now); err != nil {
		_ = os.Remove(path)
		return MediaCacheItem{}, err
	}
	expires := now.Add(c.cfg.TTL)
	return MediaCacheItem{Token: token, Path: path, Size: written, SHA256: hex.EncodeToString(hash.Sum(nil)), ExpiresAt: expires}, nil
}

func (c *MediaCache) Resolve(token string, now time.Time) (MediaCacheItem, error) {
	if !validMediaToken(token) {
		return MediaCacheItem{}, ErrMediaCacheToken
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.cleanupLocked(now); err != nil {
		return MediaCacheItem{}, err
	}
	path := filepath.Join(c.cfg.Root, token+".media")
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return MediaCacheItem{}, ErrMediaCacheMissing
		}
		return MediaCacheItem{}, err
	}
	return MediaCacheItem{Token: token, Path: path, Size: info.Size()}, nil
}

func (c *MediaCache) Cleanup(now time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cleanupLocked(now)
}

func (c *MediaCache) cleanupLocked(now time.Time) error {
	entries, err := os.ReadDir(c.cfg.Root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".media") {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if now.Sub(info.ModTime()) >= c.cfg.TTL {
			if err := os.Remove(filepath.Join(c.cfg.Root, entry.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

func (c *MediaCache) usageLocked() (int64, int, error) {
	entries, err := os.ReadDir(c.cfg.Root)
	if err != nil {
		return 0, 0, err
	}
	var bytes int64
	count := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".media") {
			continue
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return 0, 0, statErr
		}
		bytes += info.Size()
		count++
	}
	return bytes, count, nil
}

func opaqueToken() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func validMediaToken(token string) bool {
	if len(token) != 48 {
		return false
	}
	for _, char := range token {
		if !(char >= 'a' && char <= 'f') && !(char >= '0' && char <= '9') {
			return false
		}
	}
	return true
}
