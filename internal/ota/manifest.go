package ota

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const ManifestSchemaVersion = 1

type BundleManifest struct {
	SchemaVersion   int      `json:"schema_version"`
	Version         string   `json:"version"`
	MinCoreVersion  string   `json:"min_core_version"`
	CompatibleHW    []string `json:"compatible_hw"`
	BundleBytes     int64    `json:"bundle_bytes"`
	BundleSHA256    string   `json:"bundle_sha256"`
	MigrationTarget int      `json:"migration_target"`
	Signature       []byte   `json:"signature"`
}

func (m BundleManifest) signingBytes() ([]byte, error) {
	copy := m
	copy.Signature = nil
	sort.Strings(copy.CompatibleHW)
	return json.Marshal(copy)
}

func (m *BundleManifest) Sign(privateKey ed25519.PrivateKey) error {
	if m == nil || len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("OTA signing key is invalid")
	}
	data, err := m.signingBytes()
	if err != nil {
		return err
	}
	m.Signature = ed25519.Sign(privateKey, data)
	return nil
}

func VerifyBundleManifest(bundle string, manifest BundleManifest, publicKey ed25519.PublicKey, currentVersion, hardware string) error {
	if manifest.SchemaVersion != ManifestSchemaVersion || strings.TrimSpace(manifest.Version) == "" || len(publicKey) != ed25519.PublicKeySize || len(manifest.Signature) != ed25519.SignatureSize {
		return errors.New("OTA manifest is invalid")
	}
	info, err := os.Lstat(bundle)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != manifest.BundleBytes {
		return errors.New("OTA bundle size does not match manifest")
	}
	file, err := os.Open(bundle)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	sum := hash.Sum(nil)
	if hex.EncodeToString(sum) != strings.TrimSpace(manifest.BundleSHA256) {
		return errors.New("OTA bundle checksum does not match manifest")
	}
	signing, err := manifest.signingBytes()
	if err != nil || !ed25519.Verify(publicKey, signing, manifest.Signature) {
		return errors.New("OTA manifest signature verification failed")
	}
	if manifest.MinCoreVersion != "" && compareVersions(currentVersion, manifest.MinCoreVersion) < 0 {
		return fmt.Errorf("OTA requires Core %s", manifest.MinCoreVersion)
	}
	if len(manifest.CompatibleHW) > 0 && !contains(manifest.CompatibleHW, hardware) {
		return fmt.Errorf("OTA is not compatible with hardware %s", hardware)
	}
	return nil
}

func compareVersions(left, right string) int {
	parse := func(value string) []int {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(value), "v"), ".")
		result := make([]int, len(parts))
		for i, part := range parts {
			var number int
			_, _ = fmt.Sscanf(part, "%d", &number)
			result[i] = number
		}
		return result
	}
	a, b := parse(left), parse(right)
	for i := 0; i < len(a) || i < len(b); i++ {
		var av, bv int
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}
