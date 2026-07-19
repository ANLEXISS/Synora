package hypotheses

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// AssessmentVersion is an immutable interpretation of the alternatives in a
// hypothesis set. The aggregate keeps every version and exposes the last one
// as its current view.
type AssessmentVersion struct {
	Version                 uint64
	ID                      string
	Fingerprint             string
	Alternatives            []AlternativeSnapshot
	Provenance              Provenance
	CreatedAt               time.Time
	ResolutionSchemaVersion int
}

type AssessmentVersionSnapshot = AssessmentVersion

const assessmentIDPrefix = "cge-hyp-assessment-"

// DeriveAssessmentFingerprint returns the canonical semantic fingerprint of
// an assessment. Mutation provenance (time, actor, correlation, and revision)
// is deliberately absent from the canonical material.
func DeriveAssessmentFingerprint(family Family, subject Subject, alternatives []AlternativeSnapshot, provenance Provenance) (string, error) {
	schemaVersion := ResolutionSchemaLegacy
	for _, alternative := range alternatives {
		if alternative.ResolutionEffect != nil {
			schemaVersion = ResolutionSchemaV1
			break
		}
	}
	return deriveAssessmentFingerprint(family, subject, alternatives, provenance, schemaVersion)
}

func deriveAssessmentFingerprint(family Family, subject Subject, alternatives []AlternativeSnapshot, provenance Provenance, schemaVersion int) (string, error) {
	if err := family.Validate(); err != nil {
		return "", err
	}
	if err := subject.validate(family); err != nil {
		return "", err
	}
	if err := provenance.validate(family); err != nil {
		return "", err
	}
	copyAlternatives := cloneAlternatives(alternatives)
	sort.SliceStable(copyAlternatives, func(i, j int) bool {
		if copyAlternatives[i].Rank != copyAlternatives[j].Rank {
			return copyAlternatives[i].Rank < copyAlternatives[j].Rank
		}
		return copyAlternatives[i].ID < copyAlternatives[j].ID
	})
	canonical := assessmentCanonical{
		Family: family, Subject: canonicalSubject(subject),
		PolicyNamespace: provenance.PolicyNamespace, PolicyVersion: provenance.PolicyVersion,
		ResolutionSchemaVersion: schemaVersion,
		Alternatives:            canonicalAlternatives(copyAlternatives),
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", fmt.Errorf("marshal assessment fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

// DeriveAssessmentID creates the stable ID for one version within a set.
func DeriveAssessmentID(setID SetID, version uint64, fingerprint string) (string, error) {
	if err := validSetID(setID); err != nil || version == 0 || !validFingerprint(fingerprint) {
		if err == nil {
			err = fmt.Errorf("assessment version or fingerprint is invalid")
		}
		return "", err
	}
	material := fmt.Sprintf("%s\x00%d\x00%s", setID, version, fingerprint)
	digest := sha256.Sum256([]byte(material))
	return assessmentIDPrefix + hex.EncodeToString(digest[:]), nil
}

func (a AssessmentVersion) Clone() AssessmentVersion {
	a.Alternatives = cloneAlternatives(a.Alternatives)
	return a
}

// ValidateFor validates an assessment against its owning aggregate identity.
func (a AssessmentVersion) ValidateFor(family Family, subject Subject, setID SetID) error {
	return a.validate(family, subject, setID)
}

func (a AssessmentVersion) validate(family Family, subject Subject, setID SetID) error {
	if a.Version == 0 {
		return fmt.Errorf("assessment version must be positive")
	}
	if err := validText(a.ID, "assessment id", true, 128); err != nil || len(a.ID) <= len(assessmentIDPrefix) || a.ID[:len(assessmentIDPrefix)] != assessmentIDPrefix {
		if err == nil {
			err = fmt.Errorf("assessment id has invalid prefix")
		}
		return err
	}
	if !validFingerprint(a.Fingerprint) {
		return fmt.Errorf("assessment fingerprint is invalid")
	}
	if a.CreatedAt.IsZero() {
		return fmt.Errorf("assessment created timestamp is zero")
	}
	if err := a.Provenance.validate(family); err != nil {
		return err
	}
	if len(a.Alternatives) < 2 {
		return ErrInsufficientHypothesisAlternatives
	}
	if a.ResolutionSchemaVersion != ResolutionSchemaLegacy && a.ResolutionSchemaVersion != ResolutionSchemaV1 {
		return ErrInvalidResolutionSchema
	}
	copyAlternatives := cloneAlternatives(a.Alternatives)
	for i := range copyAlternatives {
		if copyAlternatives[i].Rank != i+1 {
			return fmt.Errorf("assessment alternative ranks are not contiguous")
		}
		if err := copyAlternatives[i].validate(family, subject, setID); err != nil {
			return err
		}
		if a.ResolutionSchemaVersion == ResolutionSchemaLegacy {
			if copyAlternatives[i].ResolutionEffect != nil {
				return fmt.Errorf("%w: legacy assessment contains a resolution effect", ErrInvalidResolutionSchema)
			}
		} else if err := copyAlternatives[i].ResolutionEffect.ValidateFor(family, subject, copyAlternatives[i]); err != nil {
			return err
		}
		if a.ResolutionSchemaVersion == ResolutionSchemaV1 && copyAlternatives[i].ResolutionEffect != nil && copyAlternatives[i].ResolutionEffect.AddContribution != nil {
			wantSource := "cge-evidence/" + a.Provenance.PolicyVersion
			if copyAlternatives[i].ResolutionEffect.AddContribution.ContributionTemplate.Source != wantSource {
				return fmt.Errorf("%w: contribution source does not match assessment policy", ErrResolutionContributionMismatch)
			}
		}
	}
	fingerprint, err := deriveAssessmentFingerprint(family, subject, copyAlternatives, a.Provenance, a.ResolutionSchemaVersion)
	if err != nil || fingerprint != a.Fingerprint {
		if err == nil {
			err = fmt.Errorf("assessment fingerprint does not match canonical content")
		}
		return err
	}
	expectedID, err := DeriveAssessmentID(setID, a.Version, a.Fingerprint)
	if err != nil || expectedID != a.ID {
		if err == nil {
			err = fmt.Errorf("assessment id does not match canonical content")
		}
		return err
	}
	return nil
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == hex.EncodeToString(mustDecodeHex(value))
}

func mustDecodeHex(value string) []byte {
	decoded, _ := hex.DecodeString(value)
	return decoded
}

type assessmentCanonical struct {
	Family                  Family                 `json:"family"`
	Subject                 canonicalSubjectValue  `json:"subject"`
	PolicyNamespace         string                 `json:"policy_namespace"`
	PolicyVersion           string                 `json:"policy_version"`
	ResolutionSchemaVersion int                    `json:"resolution_schema_version,omitempty"`
	Alternatives            []canonicalAlternative `json:"alternatives"`
}

type canonicalSubjectValue struct {
	ObservationID       string `json:"observation_id"`
	ChainID             string `json:"chain_id"`
	EvidenceFingerprint string `json:"evidence_fingerprint"`
}

type canonicalAlternative struct {
	ID                  string            `json:"id"`
	Kind                AlternativeKind   `json:"kind"`
	ChainID             string            `json:"chain_id"`
	SourceRevision      uint64            `json:"source_revision"`
	Score               int64             `json:"score"`
	Rank                int               `json:"rank"`
	ReasonCode          string            `json:"reason_code"`
	Facts               []canonicalFact   `json:"facts"`
	ContributionID      string            `json:"contribution_id"`
	EvidenceFingerprint string            `json:"evidence_fingerprint"`
	ResolutionEffect    *ResolutionEffect `json:"resolution_effect,omitempty"`
}

type canonicalFact struct {
	Code           string   `json:"code"`
	Side           string   `json:"side"`
	Score          int64    `json:"score"`
	ObservationIDs []string `json:"observation_ids"`
}

func canonicalSubject(subject Subject) canonicalSubjectValue {
	return canonicalSubjectValue{ObservationID: subject.ObservationID, ChainID: string(subject.ChainID), EvidenceFingerprint: subject.EvidenceFingerprint}
}

func canonicalAlternatives(alternatives []Alternative) []canonicalAlternative {
	result := make([]canonicalAlternative, len(alternatives))
	for i, alternative := range alternatives {
		facts := cloneFacts(alternative.Facts)
		for j := range facts {
			sort.Strings(facts[j].ObservationIDs)
		}
		sort.SliceStable(facts, func(i, j int) bool {
			if facts[i].Code != facts[j].Code {
				return facts[i].Code < facts[j].Code
			}
			if facts[i].Side != facts[j].Side {
				return facts[i].Side < facts[j].Side
			}
			return facts[i].Score < facts[j].Score
		})
		canonicalFacts := make([]canonicalFact, len(facts))
		for j, fact := range facts {
			canonicalFacts[j] = canonicalFact{Code: fact.Code, Side: fact.Side, Score: fact.Score, ObservationIDs: append([]string(nil), fact.ObservationIDs...)}
		}
		result[i] = canonicalAlternative{ID: alternative.ID, Kind: alternative.Kind, ChainID: string(alternative.ChainID), SourceRevision: alternative.SourceRevision, Score: alternative.Score, Rank: alternative.Rank, ReasonCode: alternative.ReasonCode, Facts: canonicalFacts, ContributionID: alternative.ContributionID, EvidenceFingerprint: alternative.EvidenceFingerprint, ResolutionEffect: cloneResolutionEffect(alternative.ResolutionEffect)}
	}
	return result
}
