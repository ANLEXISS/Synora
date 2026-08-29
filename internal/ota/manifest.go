package ota

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
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
	SchemaVersion      int      `json:"schema_version"`
	ProductID          string   `json:"product_id,omitempty"`
	Target             string   `json:"target,omitempty"`
	Version            string   `json:"version"`
	SecurityGeneration uint64   `json:"security_generation,omitempty"`
	MinCoreVersion     string   `json:"min_core_version"`
	CompatibleHW       []string `json:"compatible_hw"`
	BundleBytes        int64    `json:"bundle_bytes"`
	BundleSHA256       string   `json:"bundle_sha256"`
	MigrationTarget    int      `json:"migration_target"`
	Signature          []byte   `json:"signature"`
	SignatureAlgorithm string   `json:"signature_algorithm,omitempty"`
	ReleaseSignature   []byte   `json:"release_signature,omitempty"`
	SignerCertificate  []byte   `json:"signer_certificate,omitempty"`
	Signer             string   `json:"signer,omitempty"`
}

func (m BundleManifest) signingBytes() ([]byte, error) {
	copy := m
	copy.Signature = nil
	copy.ReleaseSignature = nil
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

// SignRelease signs the detached Synora manifest with a short-lived RSA
// release certificate. RAUC independently verifies the same certificate
// chain when it installs the bundle; this signature makes the detached
// manifest self-describing for offline preflight and tests.
func (m *BundleManifest) SignRelease(privateKey *rsa.PrivateKey, certificate *x509.Certificate) error {
	if m == nil || privateKey == nil || privateKey.N.BitLen() < 3072 || certificate == nil {
		return errors.New("OTA release signing key is invalid")
	}
	m.Signature = nil
	m.SignatureAlgorithm = ReleaseSignatureAlgorithm
	m.SignerCertificate = certificate.Raw
	m.Signer = certificate.Subject.CommonName
	data, err := m.signingBytes()
	if err != nil {
		return err
	}
	digest := sha256.Sum256(data)
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, cryptoHashSHA256, digest[:])
	if err != nil {
		return err
	}
	m.ReleaseSignature = signature
	return nil
}

func VerifyBundleManifest(bundle string, manifest BundleManifest, publicKey ed25519.PublicKey, currentVersion, hardware string) error {
	return verifyBundleManifest(bundle, manifest, publicKey, currentVersion, hardware, 0)
}

// VerifyReleaseBundleManifest validates the production RSA/X.509 release
// profile before a central or camera installer hands the bundle to RAUC.
func VerifyReleaseBundleManifest(bundle string, manifest BundleManifest, profile *ReleasePKI, currentVersion, hardware string, currentMigration int, currentGeneration uint64) error {
	return verifyBundleManifestWithPolicy(bundle, manifest, nil, currentVersion, hardware, currentMigration, currentGeneration, profile)
}

func verifyBundleManifest(bundle string, manifest BundleManifest, publicKey ed25519.PublicKey, currentVersion, hardware string, currentMigration int) error {
	return verifyBundleManifestWithPolicy(bundle, manifest, publicKey, currentVersion, hardware, currentMigration, 0, nil)
}

func verifyBundleManifestWithPolicy(bundle string, manifest BundleManifest, publicKey ed25519.PublicKey, currentVersion, hardware string, currentMigration int, currentGeneration uint64, pki *ReleasePKI) error {
	if manifest.SchemaVersion != ManifestSchemaVersion || strings.TrimSpace(manifest.Version) == "" || len(publicKey) != ed25519.PublicKeySize || len(manifest.Signature) != ed25519.SignatureSize {
		if pki == nil || manifest.SchemaVersion != ManifestSchemaVersion || strings.TrimSpace(manifest.Version) == "" {
			return errors.New("OTA manifest is invalid")
		}
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
	if err != nil {
		return err
	}
	if pki != nil {
		if err := pki.VerifyManifest(manifest, signing); err != nil {
			return err
		}
	} else if !ed25519.Verify(publicKey, signing, manifest.Signature) {
		return errors.New("OTA manifest signature verification failed")
	}
	if manifest.MinCoreVersion != "" && compareVersions(currentVersion, manifest.MinCoreVersion) < 0 {
		return fmt.Errorf("OTA requires Core %s", manifest.MinCoreVersion)
	}
	if strings.TrimSpace(currentVersion) != "" && compareVersions(manifest.Version, currentVersion) < 0 {
		return fmt.Errorf("OTA downgrade refused: current Core is %s, candidate is %s", currentVersion, manifest.Version)
	}
	if manifest.SecurityGeneration < currentGeneration {
		return fmt.Errorf("OTA security generation downgrade refused: current is %d, candidate is %d", currentGeneration, manifest.SecurityGeneration)
	}
	if currentMigration > 0 && manifest.MigrationTarget < currentMigration {
		return fmt.Errorf("OTA migration downgrade refused: current schema is %d, candidate targets %d", currentMigration, manifest.MigrationTarget)
	}
	if len(manifest.CompatibleHW) > 0 && !contains(manifest.CompatibleHW, hardware) {
		return fmt.Errorf("OTA is not compatible with hardware %s", hardware)
	}
	return nil
}

const cryptoHashSHA256 = crypto.Hash(crypto.SHA256)

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
