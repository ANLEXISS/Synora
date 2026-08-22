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
