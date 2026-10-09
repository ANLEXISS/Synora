package facegallery

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const ConsentSchemaVersion = "face-gallery-consent/v1"

var consentRefPattern = regexp.MustCompile(`^res_[a-f0-9]{32}$`)

type Attestation struct {
	SchemaVersion        string     `json:"schema_version"`
	ResidentRef          string     `json:"resident_ref"`
	Purpose              string     `json:"purpose"`
	SourceSetID          string     `json:"source_set_id"`
	SourceSetDigest      string     `json:"source_set_digest"`
	IssuedAt             time.Time  `json:"issued_at"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	Revoked              bool       `json:"revoked"`
	PolicyVersion        string     `json:"policy_version"`
	AttestationReference string     `json:"attestation_reference"`
	Provenance           string     `json:"provenance"`
}

type SourceSet struct {
	ID     string
	Digest string
}

var ErrConsentUnverified = errors.New("consent_unverified")

// ValidateAttestation validates a technical local assertion. It does not
// claim to establish legal consent or identify the person represented.
func ValidateAttestation(value Attestation, residentRef string, allowed map[string]SourceSet, now time.Time) error {
	if value.Provenance == "simulated_test" {
		return ErrConsentUnverified
	}
	if value.Provenance != "local_attestation" || value.SchemaVersion != ConsentSchemaVersion ||
		value.ResidentRef != residentRef || !consentRefPattern.MatchString(value.ResidentRef) ||
		value.Purpose != "face_gallery_enrollment" || value.PolicyVersion != "face-gallery-policy/v1" ||
		value.AttestationReference == "" || len(value.AttestationReference) > 96 ||
		strings.ContainsAny(value.AttestationReference, "/\\\n\r") || value.Revoked || value.IssuedAt.IsZero() {
		return ErrConsentUnverified
	}
	set, ok := allowed[value.SourceSetID]
	if !ok || set.ID != value.SourceSetID || set.Digest != value.SourceSetDigest || set.Digest == "" {
		return ErrConsentUnverified
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if value.IssuedAt.After(now.UTC().Add(time.Minute)) || value.ExpiresAt != nil && !now.UTC().Before(value.ExpiresAt.UTC()) {
		return ErrConsentUnverified
	}
	return nil
}

// LoadAttestation resolves a server-owned path from an opaque resident ref.
// Callers never supply a filesystem path or URL.
func LoadAttestation(root, residentRef string) (Attestation, error) {
	if !filepath.IsAbs(root) || !consentRefPattern.MatchString(residentRef) {
		return Attestation{}, ErrConsentUnverified
	}
	rootInfo, err := os.Lstat(root)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 || rootInfo.Mode().Perm()&0o077 != 0 {
		return Attestation{}, ErrConsentUnverified
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return Attestation{}, ErrConsentUnverified
	}
	path := filepath.Join(root, residentRef+".json")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > 16*1024 {
		return Attestation{}, ErrConsentUnverified
	}
	file, err := os.Open(path)
	if err != nil {
		return Attestation{}, ErrConsentUnverified
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16*1024))
	decoder.DisallowUnknownFields()
	var value Attestation
	if decoder.Decode(&value) != nil {
		return Attestation{}, ErrConsentUnverified
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return Attestation{}, ErrConsentUnverified
	}
	canonical, err := json.Marshal(value)
	if err != nil || len(bytes.TrimSpace(canonical)) == 0 {
		return Attestation{}, ErrConsentUnverified
	}
	return value, nil
}
