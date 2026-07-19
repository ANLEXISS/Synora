package hypotheses

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"synora/internal/cge/chains"
)

const (
	associationNamespace = "synora.cge.hypotheses.association"
	evidenceNamespace    = "synora.cge.hypotheses.evidence"
)

func (k AlternativeKind) Validate() error {
	switch k {
	case AlternativeAttachExisting, AlternativeCreateCandidate, AlternativeSupport, AlternativeContradiction, AlternativeNeutral, AlternativeInsufficient:
		return nil
	default:
		return fmt.Errorf("invalid hypothesis alternative kind %q", k)
	}
}

func (f FactReference) validate() error {
	if err := validText(f.Code, "fact code", true, 64); err != nil {
		return err
	}
	if err := validText(f.Side, "fact side", true, 32); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(f.ObservationIDs))
	for _, id := range f.ObservationIDs {
		if err := validText(id, "fact observation id", true, 256); err != nil {
			return err
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate fact observation id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (p Provenance) validate(family Family) error {
	if err := family.Validate(); err != nil {
		return err
	}
	wantSource := string(family)
	if p.Source != wantSource || p.PlannedOrEvaluatedAt.IsZero() {
		return fmt.Errorf("hypothesis provenance source or timestamp is invalid")
	}
	if err := validText(p.PolicyNamespace, "policy namespace", true, 128); err != nil {
		return err
	}
	if err := validText(p.PolicyVersion, "policy version", true, 128); err != nil {
		return err
	}
	if family == FamilyEvidence && p.SourceRevision == 0 {
		return fmt.Errorf("evidence provenance source revision must be positive")
	}
	return nil
}

func (s Subject) validate(family Family) error {
	if err := family.Validate(); err != nil {
		return err
	}
	if err := validText(s.ObservationID, "subject observation id", true, 256); err != nil {
		return err
	}
	switch family {
	case FamilyAssociation:
		if s.ChainID != "" || s.EvidenceFingerprint != "" {
			return fmt.Errorf("association subject must not contain chain or evidence fingerprint")
		}
	case FamilyEvidence:
		if _, err := chains.NewChainID(string(s.ChainID)); err != nil {
			return err
		}
		if err := validText(s.EvidenceFingerprint, "subject evidence fingerprint", true, 256); err != nil {
			return err
		}
	}
	return nil
}

func (a Alternative) validate(family Family, subject Subject, setID SetID) error {
	if err := validText(a.ID, "alternative id", true, 128); err != nil {
		return err
	}
	if err := a.Kind.Validate(); err != nil {
		return err
	}
	if a.Score < 0 || a.Rank <= 0 {
		return fmt.Errorf("alternative score or rank is invalid")
	}
	if err := validText(a.ReasonCode, "alternative reason code", true, 64); err != nil {
		return err
	}
	for _, fact := range a.Facts {
		if err := fact.validate(); err != nil {
			return err
		}
	}
	if err := validText(a.ContributionID, "contribution id", false, 256); err != nil {
		return err
	}
	if err := validText(a.EvidenceFingerprint, "evidence fingerprint", false, 256); err != nil {
		return err
	}
	switch family {
	case FamilyAssociation:
		if a.Kind != AlternativeAttachExisting && a.Kind != AlternativeCreateCandidate {
			return fmt.Errorf("association alternative kind is invalid")
		}
		if _, err := chains.NewChainID(string(a.ChainID)); err != nil {
			return err
		}
		if a.Kind == AlternativeAttachExisting && a.SourceRevision == 0 {
			return fmt.Errorf("existing-chain alternative revision must be positive")
		}
		if a.Kind == AlternativeCreateCandidate && a.SourceRevision != 0 {
			return fmt.Errorf("candidate-creation alternative must not have a source revision")
		}
		if a.ContributionID != "" || a.EvidenceFingerprint != "" {
			return fmt.Errorf("association alternative contains evidence fields")
		}
	case FamilyEvidence:
		if a.Kind != AlternativeSupport && a.Kind != AlternativeContradiction && a.Kind != AlternativeNeutral && a.Kind != AlternativeInsufficient {
			return fmt.Errorf("evidence alternative kind is invalid")
		}
		if a.ChainID != subject.ChainID || a.SourceRevision == 0 || a.EvidenceFingerprint != subject.EvidenceFingerprint {
			return fmt.Errorf("evidence alternative subject is inconsistent")
		}
	}
	expected := deriveAlternativeID(setID, a)
	if a.ID != expected {
		return fmt.Errorf("alternative id does not match deterministic derivation")
	}
	return nil
}

func deriveAlternativeID(setID SetID, alternative Alternative) string {
	material := strings.Join([]string{"synora.cge.hypotheses.alternative", string(setID), string(alternative.Kind), string(alternative.ChainID), fmt.Sprint(alternative.SourceRevision), alternative.ContributionID, alternative.EvidenceFingerprint, fmt.Sprint(alternative.Rank)}, "\x00")
	digest := sha256.Sum256([]byte(material))
	return "cge-hyp-alt-" + hex.EncodeToString(digest[:])
}
