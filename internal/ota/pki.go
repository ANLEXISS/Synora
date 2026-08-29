package ota

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	TargetCentral = "central"
	TargetCamera  = "camera"

	ReleaseSignatureAlgorithm = "rsa-pkcs1v15-sha256"
)

// ReleasePKI is the parsed, target-specific trust profile used for detached
// release preflight. RAUC remains the installation authority and must be
// configured with the same roots, CRL policy and target separation.
type ReleasePKI struct {
	Target        string
	ProductID     string
	Roots         []*x509.Certificate
	Intermediates []*x509.Certificate
	CRLs          []*x509.RevocationList
}

func NewReleasePKI(target, productID string, rootsPEM, intermediatesPEM, crlPEM []byte) (*ReleasePKI, error) {
	target = strings.TrimSpace(target)
	if target != TargetCentral && target != TargetCamera {
		return nil, fmt.Errorf("OTA release target is invalid: %q", target)
	}
	if strings.TrimSpace(productID) == "" {
		return nil, errors.New("OTA release product id is required")
	}
	roots, err := parseCertificates(rootsPEM)
	if err != nil || len(roots) == 0 {
		return nil, fmt.Errorf("OTA release roots are invalid: %w", err)
	}
	intermediates, err := parseCertificates(intermediatesPEM)
	if err != nil {
		return nil, fmt.Errorf("OTA release intermediates are invalid: %w", err)
	}
	crls, err := parseCRLs(crlPEM)
	if err != nil {
		return nil, fmt.Errorf("OTA release CRLs are invalid: %w", err)
	}
	profile := &ReleasePKI{Target: target, ProductID: strings.TrimSpace(productID), Roots: roots, Intermediates: intermediates, CRLs: crls}
	if err := profile.validateAuthorities(); err != nil {
		return nil, err
	}
	return profile, nil
}

func (p *ReleasePKI) validateAuthorities() error {
	for _, root := range p.Roots {
		if !root.IsCA || !root.BasicConstraintsValid || rsaBits(root) < 4096 {
			return errors.New("OTA release root must be an RSA 4096 CA")
		}
	}
	for _, intermediate := range p.Intermediates {
		if !intermediate.IsCA || !intermediate.BasicConstraintsValid || rsaBits(intermediate) < 3072 {
			return errors.New("OTA release intermediate must be an RSA 3072 CA")
		}
	}
	return nil
}

func (p *ReleasePKI) VerifyManifest(manifest BundleManifest, signingBytes []byte) error {
	if p == nil {
		return errors.New("OTA release trust profile unavailable")
	}
	if strings.TrimSpace(manifest.ProductID) != p.ProductID || strings.TrimSpace(manifest.Target) != p.Target {
		return fmt.Errorf("OTA release target or product mismatch")
	}
	if manifest.SignatureAlgorithm != ReleaseSignatureAlgorithm || len(manifest.ReleaseSignature) == 0 || len(manifest.SignerCertificate) == 0 {
		return errors.New("OTA release signature is missing")
	}
	certificate, err := x509.ParseCertificate(manifest.SignerCertificate)
	if err != nil {
		return errors.New("OTA release signer certificate is invalid")
	}
	if certificate.IsCA || !certificate.BasicConstraintsValid || rsaBits(certificate) < 3072 || certificate.KeyUsage&x509.KeyUsageDigitalSignature == 0 || !hasCodeSigningUsage(certificate) {
		return errors.New("OTA release signer certificate is not code-signing restricted")
	}
	if manifest.Signer != "" && strings.TrimSpace(manifest.Signer) != certificate.Subject.CommonName {
		return errors.New("OTA release signer identity mismatch")
	}
	if err := p.verifyCertificate(certificate); err != nil {
		return err
	}
	key, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("OTA release signer key is not RSA")
	}
	digest := sha256Digest(signingBytes)
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest, manifest.ReleaseSignature); err != nil {
		return errors.New("OTA release signature verification failed")
	}
	return nil
}

func (p *ReleasePKI) verifyCertificate(certificate *x509.Certificate) error {
	intermediates := x509.NewCertPool()
	for _, cert := range p.Intermediates {
		intermediates.AddCert(cert)
	}
	roots := x509.NewCertPool()
	for _, cert := range p.Roots {
		roots.AddCert(cert)
	}
	chains, err := certificate.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		CurrentTime:   time.Now().UTC(),
	})
	if err != nil || len(chains) == 0 {
		return errors.New("OTA release signer chain verification failed")
	}
	for _, chain := range chains[0] {
		if revoked(chain, p.CRLs) {
			return errors.New("OTA release signer is revoked")
		}
	}
	if certificate.NotAfter.Sub(certificate.NotBefore) > 366*24*time.Hour {
		return errors.New("OTA release signer certificate validity exceeds 12 months")
	}
	return nil
}

func parseCertificates(data []byte) ([]*x509.Certificate, error) {
	var result []*x509.Certificate
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			if strings.TrimSpace(string(data)) == "" {
				break
			}
			return nil, errors.New("PEM certificate expected")
		}
		data = rest
		if block.Type != "CERTIFICATE" {
			return nil, errors.New("PEM certificate expected")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		result = append(result, certificate)
	}
	return result, nil
}

func parseCRLs(data []byte) ([]*x509.RevocationList, error) {
	var result []*x509.RevocationList
	for len(data) > 0 {
		block, rest := pem.Decode(data)
		if block == nil {
			if strings.TrimSpace(string(data)) == "" {
				break
			}
			return nil, errors.New("PEM CRL expected")
		}
		data = rest
		if block.Type != "X509 CRL" {
			return nil, errors.New("PEM CRL expected")
		}
		crl, err := x509.ParseRevocationList(block.Bytes)
		if err != nil {
			return nil, err
		}
		result = append(result, crl)
	}
	return result, nil
}

func revoked(certificate *x509.Certificate, crls []*x509.RevocationList) bool {
	for _, crl := range crls {
		if crl.Issuer.String() != certificate.Issuer.String() {
			continue
		}
		for _, entry := range crl.RevokedCertificateEntries {
			if entry.SerialNumber.Cmp(certificate.SerialNumber) == 0 {
				return true
			}
		}
	}
	return false
}

func hasCodeSigningUsage(certificate *x509.Certificate) bool {
	for _, usage := range certificate.ExtKeyUsage {
		if usage == x509.ExtKeyUsageCodeSigning {
			return true
		}
	}
	return false
}

func rsaBits(certificate *x509.Certificate) int {
	key, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok || key == nil {
		return 0
	}
	return key.N.BitLen()
}

func sha256Digest(data []byte) []byte {
	digest := sha256.Sum256(data)
	return digest[:]
}
