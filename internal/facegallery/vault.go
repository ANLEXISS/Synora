package facegallery

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const VaultManifestSchema = "face-gallery-vault/v1"

type VaultManifest struct {
	SchemaVersion string `json:"schema_version"`
	ResidentRef   string `json:"resident_ref"`
	Generation    uint64 `json:"gallery_generation"`
	Previous      uint64 `json:"previous_generation,omitempty"`
	Entries       []any  `json:"entries"`
	Digest        string `json:"manifest_sha256"`
}

type Vault struct{ root string }

func NewVault(root string) (*Vault, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, errors.New("vault root must be an absolute canonical path")
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return nil, err
	}
	return &Vault{root: root}, nil
}

func ensurePrivateDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return errors.New("vault permissions or directory type invalid")
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return errors.New("vault root resolves through a symlink")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return ensurePrivateDirectory(path)
}

func (v *Vault) Initialize(residentRef string) (VaultManifest, error) {
	if v == nil || !consentRefPattern.MatchString(residentRef) {
		return VaultManifest{}, errors.New("invalid vault operation")
	}
	dir, err := v.residentDir(residentRef, true)
	if err != nil {
		return VaultManifest{}, err
	}
	if generation, err := readCurrent(dir); err == nil {
		return readManifest(dir, residentRef, generation)
	} else if !errors.Is(err, os.ErrNotExist) {
		return VaultManifest{}, err
	}
	manifest := VaultManifest{SchemaVersion: VaultManifestSchema, ResidentRef: residentRef, Generation: 1, Entries: []any{}}
	if err := writeManifest(dir, manifest); err != nil {
		return VaultManifest{}, err
	}
	if err := atomicWrite(filepath.Join(dir, "CURRENT"), []byte("1\n"), 0o600); err != nil {
		return VaultManifest{}, err
	}
	return readManifest(dir, residentRef, 1)
}

func (v *Vault) Current(residentRef string) (VaultManifest, error) {
	if v == nil || !consentRefPattern.MatchString(residentRef) {
		return VaultManifest{}, errors.New("invalid vault operation")
	}
	dir, err := v.residentDir(residentRef, false)
	if err != nil {
		return VaultManifest{}, err
	}
	generation, err := readCurrent(dir)
	if err != nil {
		return VaultManifest{}, err
	}
	return readManifest(dir, residentRef, generation)
}

func (v *Vault) residentDir(residentRef string, create bool) (string, error) {
	if v == nil || !consentRefPattern.MatchString(residentRef) {
		return "", errors.New("invalid vault operation")
	}
	if err := ensurePrivateDirectory(v.root); err != nil {
		return "", err
	}
	dir := filepath.Join(v.root, residentRef)
	if create {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("resident vault directory invalid")
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || resolved != dir {
		return "", errors.New("resident vault directory resolves through a symlink")
	}
	return dir, nil
}

// CommitEmptyGeneration versions policy/state metadata while keeping the
// biometric entry set empty. It is intended for administrative lifecycle
// tests until a separately qualified enrollment workflow exists.
func (v *Vault) CommitEmptyGeneration(residentRef string) (VaultManifest, error) {
	current, err := v.Current(residentRef)
	if err != nil {
		return VaultManifest{}, err
	}
	next := VaultManifest{SchemaVersion: VaultManifestSchema, ResidentRef: residentRef, Generation: current.Generation + 1, Previous: current.Generation, Entries: []any{}}
	dir := filepath.Join(v.root, residentRef)
	path := filepath.Join(dir, fmt.Sprintf("generation-%d.json", next.Generation))
	if _, err := os.Lstat(path); err == nil {
		return VaultManifest{}, errors.New("gallery generation already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return VaultManifest{}, err
	}
	if err := writeManifest(dir, next); err != nil {
		return VaultManifest{}, err
	}
	if err := atomicWrite(filepath.Join(dir, "CURRENT"), []byte(strconv.FormatUint(next.Generation, 10)+"\n"), 0o600); err != nil {
		return VaultManifest{}, err
	}
	return readManifest(dir, residentRef, next.Generation)
}

// Rollback moves the active pointer to the previous empty generation. It is
// idempotent at generation 1 and never deletes or imports biometric material.
func (v *Vault) Rollback(residentRef string) (VaultManifest, bool, error) {
	current, err := v.Current(residentRef)
	if err != nil {
		return VaultManifest{}, false, err
	}
	if current.Generation <= 1 {
		return current, true, nil
	}
	previous := current.Generation - 1
	manifest, err := readManifest(filepath.Join(v.root, residentRef), residentRef, previous)
	if err != nil {
		return VaultManifest{}, false, err
	}
	if err := atomicWrite(filepath.Join(v.root, residentRef, "CURRENT"), []byte(strconv.FormatUint(previous, 10)+"\n"), 0o600); err != nil {
		return VaultManifest{}, false, err
	}
	return manifest, false, nil
}

func readCurrent(dir string) (uint64, error) {
	info, err := os.Lstat(filepath.Join(dir, "CURRENT"))
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		if err != nil {
			return 0, err
		}
		return 0, errors.New("vault pointer invalid")
	}
	body, err := os.ReadFile(filepath.Join(dir, "CURRENT"))
	if err != nil {
		return 0, err
	}
	generation, err := strconv.ParseUint(strings.TrimSpace(string(body)), 10, 64)
	if err != nil || generation == 0 {
		return 0, errors.New("vault pointer invalid")
	}
	return generation, nil
}

func readManifest(dir, residentRef string, generation uint64) (VaultManifest, error) {
	path := filepath.Join(dir, fmt.Sprintf("generation-%d.json", generation))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() > 64*1024 {
		return VaultManifest{}, errors.New("vault manifest unavailable")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return VaultManifest{}, errors.New("vault manifest unavailable")
	}
	var manifest VaultManifest
	if json.Unmarshal(body, &manifest) != nil || manifest.SchemaVersion != VaultManifestSchema || manifest.ResidentRef != residentRef || manifest.Generation != generation || manifest.Entries == nil {
		return VaultManifest{}, errors.New("vault manifest invalid")
	}
	digest := manifest.Digest
	manifest.Digest = ""
	canonical, _ := json.Marshal(manifest)
	actual := sha256.Sum256(canonical)
	if digest != hex.EncodeToString(actual[:]) {
		return VaultManifest{}, errors.New("vault manifest integrity failure")
	}
	manifest.Digest = digest
	return manifest, nil
}

func writeManifest(dir string, manifest VaultManifest) error {
	manifest.Digest = ""
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(canonical)
	manifest.Digest = hex.EncodeToString(digest[:])
	body, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("generation-%d.json", manifest.Generation))
	return atomicWrite(path, append(body, '\n'), 0o600)
}

func atomicWrite(path string, body []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gallery-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if dirFile, err := os.Open(dir); err == nil {
		_ = dirFile.Sync()
		return dirFile.Close()
	}
	return nil
}
