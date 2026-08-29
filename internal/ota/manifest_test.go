package ota

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

func TestBundleManifestVerifiesSignatureChecksumAndCompatibility(t *testing.T) {
	bundle := t.TempDir() + "/update.raucb"
	data := []byte("signed update payload")
	if err := os.WriteFile(bundle, data, 0o600); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	sum := sha256.Sum256(data)
	manifest := BundleManifest{SchemaVersion: ManifestSchemaVersion, Version: "1.4.0", MinCoreVersion: "1.2.0", CompatibleHW: []string{"rock-5b"}, BundleBytes: int64(len(data)), BundleSHA256: hex.EncodeToString(sum[:]), MigrationTarget: 3}
	if err := manifest.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundleManifest(bundle, manifest, publicKey, "1.3.0", "rock-5b"); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundleManifest(bundle, manifest, publicKey, "1.1.0", "rock-5b"); err == nil {
		t.Fatal("incompatible Core accepted")
	}
	if err := os.WriteFile(bundle, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundleManifest(bundle, manifest, publicKey, "1.3.0", "rock-5b"); err == nil {
		t.Fatal("tampered bundle accepted")
	}
}

func TestBundleManifestRejectsUnsignedInvalidAndDowngradeCandidates(t *testing.T) {
	bundle := t.TempDir() + "/update.raucb"
	data := []byte("signed update payload")
	if err := os.WriteFile(bundle, data, 0o600); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	sum := sha256.Sum256(data)
	manifest := BundleManifest{SchemaVersion: ManifestSchemaVersion, Version: "1.4.0", BundleBytes: int64(len(data)), BundleSHA256: hex.EncodeToString(sum[:]), MigrationTarget: 3}
	if err := manifest.Sign(privateKey); err != nil {
		t.Fatal(err)
	}

	unsigned := manifest
	unsigned.Signature = nil
	if err := VerifyBundleManifest(bundle, unsigned, publicKey, "1.3.0", "rock-5b"); err == nil {
		t.Fatal("unsigned manifest accepted")
	}
	invalidSignature := manifest
	invalidSignature.Signature = append([]byte(nil), manifest.Signature...)
	invalidSignature.Signature[0] ^= 0xff
	if err := VerifyBundleManifest(bundle, invalidSignature, publicKey, "1.3.0", "rock-5b"); err == nil {
		t.Fatal("invalid manifest signature accepted")
	}

	downgrade := manifest
	downgrade.Version = "1.2.0"
	if err := downgrade.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if err := VerifyBundleManifest(bundle, downgrade, publicKey, "1.3.0", "rock-5b"); err == nil {
		t.Fatal("version downgrade accepted")
	}
	if err := verifyBundleManifest(bundle, manifest, publicKey, "1.3.0", "rock-5b", 4); err == nil {
		t.Fatal("migration downgrade accepted")
	}
	if err := verifyBundleManifestWithPolicy(bundle, manifest, publicKey, "1.3.0", "rock-5b", 2, 4, nil); err == nil {
		t.Fatal("security generation downgrade accepted")
	}
}
