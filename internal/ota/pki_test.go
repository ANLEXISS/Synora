package ota

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type certificateFixture struct {
	root, intermediate, release          *x509.Certificate
	rootKey, intermediateKey, releaseKey *rsa.PrivateKey
}

func newCertificateFixture(t *testing.T) certificateFixture {
	t.Helper()
	now := time.Now().UTC().Add(-time.Minute)
	rootKey := generateRSA(t, 4096)
	root := createCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synora Production Root"},
		NotBefore: now, NotAfter: now.Add(10 * 365 * 24 * time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, nil, rootKey, rootKey)
	intermediateKey := generateRSA(t, 3072)
	intermediate := createCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "Synora Central Release"},
		NotBefore: now, NotAfter: now.Add(5 * 365 * 24 * time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}, root, intermediateKey, rootKey)
	releaseKey := generateRSA(t, 3072)
	release := createCertificate(t, &x509.Certificate{
		SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "central-release-2026"},
		NotBefore: now, NotAfter: now.Add(365 * 24 * time.Hour),
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
	}, intermediate, releaseKey, intermediateKey)
	return certificateFixture{root: root, intermediate: intermediate, release: release, rootKey: rootKey, intermediateKey: intermediateKey, releaseKey: releaseKey}
}

func generateRSA(t *testing.T, bits int) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func createCertificate(t *testing.T, template, parent *x509.Certificate, subjectKey, signerKey *rsa.PrivateKey) *x509.Certificate {
	t.Helper()
	if parent == nil {
		parent = template
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, subjectKey.Public(), signerKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate
}

func certPEM(certificates ...*x509.Certificate) []byte {
	var data []byte
	for _, certificate := range certificates {
		data = append(data, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})...)
	}
	return data
}

func releaseManifestFixture(t *testing.T, fixture certificateFixture) (BundleManifest, string) {
	t.Helper()
	bundle := filepath.Join(t.TempDir(), "release.raucb")
	data := []byte("release payload")
	if err := os.WriteFile(bundle, data, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	manifest := BundleManifest{
		SchemaVersion: ManifestSchemaVersion, ProductID: "synora-central", Target: TargetCentral,
		Version: "1.5.0", SecurityGeneration: 5, CompatibleHW: []string{"rock-5-itx"},
		BundleBytes: int64(len(data)), BundleSHA256: hex.EncodeToString(sum[:]), MigrationTarget: 3,
	}
	if err := manifest.SignRelease(fixture.releaseKey, fixture.release); err != nil {
		t.Fatal(err)
	}
	return manifest, bundle
}

func TestReleasePKIRejectsInvalidTargetMutationAndRevocation(t *testing.T) {
	fixture := newCertificateFixture(t)
	manifest, bundle := releaseManifestFixture(t, fixture)
	profile, err := NewReleasePKI(TargetCentral, "synora-central", certPEM(fixture.root), certPEM(fixture.intermediate), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyBundleManifestWithPolicy(bundle, manifest, nil, "1.4.0", "rock-5-itx", 2, 4, profile); err != nil {
		t.Fatal(err)
	}

	wrongTarget := manifest
	wrongTarget.Target = TargetCamera
	if err := verifyBundleManifestWithPolicy(bundle, wrongTarget, nil, "1.4.0", "rock-5-itx", 2, 4, profile); err == nil {
		t.Fatal("wrong target accepted")
	}
	modified := manifest
	modified.ReleaseSignature[0] ^= 0xff
	if err := verifyBundleManifestWithPolicy(bundle, modified, nil, "1.4.0", "rock-5-itx", 2, 4, profile); err == nil {
		t.Fatal("modified signature accepted")
	}

	crlDER, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number: big.NewInt(8), ThisUpdate: time.Now().UTC().Add(-time.Minute),
		NextUpdate:                time.Now().UTC().Add(time.Hour),
		RevokedCertificateEntries: []x509.RevocationListEntry{{SerialNumber: fixture.release.SerialNumber, RevocationTime: time.Now().UTC()}},
	}, fixture.intermediate, fixture.intermediateKey)
	if err != nil {
		t.Fatal(err)
	}
	revokedProfile, err := NewReleasePKI(TargetCentral, "synora-central", certPEM(fixture.root), certPEM(fixture.intermediate), pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: crlDER}))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyBundleManifestWithPolicy(bundle, manifest, nil, "1.4.0", "rock-5-itx", 2, 4, revokedProfile); err == nil {
		t.Fatal("revoked signer accepted")
	}
}

func TestReleasePKIAllowsProgressiveRootRotation(t *testing.T) {
	oldFixture := newCertificateFixture(t)
	newFixture := newCertificateFixture(t)
	manifest, bundle := releaseManifestFixture(t, newFixture)
	profile, err := NewReleasePKI(TargetCentral, "synora-central", certPEM(oldFixture.root, newFixture.root), certPEM(oldFixture.intermediate, newFixture.intermediate), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyBundleManifestWithPolicy(bundle, manifest, nil, "1.4.0", "rock-5-itx", 2, 4, profile); err != nil {
		t.Fatal(err)
	}
}
