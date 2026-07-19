package evidence

import (
	"errors"
	"fmt"
	"strings"
)

// EvidenceSide identifies which independent score a fact contributes to.
type EvidenceSide string

const (
	EvidenceSupport       EvidenceSide = "support"
	EvidenceContradiction EvidenceSide = "contradiction"
	EvidenceNeutral       EvidenceSide = "neutral"
)

func (s EvidenceSide) Validate() error {
	switch s {
	case EvidenceSupport, EvidenceContradiction, EvidenceNeutral:
		return nil
	default:
		return fmt.Errorf("invalid evidence side %q", s)
	}
}

// EvidenceFact is a bounded, explainable scoring component. Neutral facts may
// carry a negative score only for the explicit uncertain-evidence penalty; the
// evaluator applies that penalty to both directional scores and clamps them
// at zero. This keeps penalties visible without hiding a signed adjustment in
// a support or contradiction fact.
type EvidenceFact struct {
	Code           string
	Side           EvidenceSide
	Score          int64
	ObservationIDs []string
	Detail         string
}

func (f EvidenceFact) Validate() error {
	if strings.TrimSpace(f.Code) == "" || strings.ContainsAny(f.Code, "\r\n") {
		return errors.New("evidence fact code must be a non-empty single line")
	}
	if err := f.Side.Validate(); err != nil {
		return err
	}
	if f.Score < 0 && !(f.Side == EvidenceNeutral && f.Code == "type.uncertain_penalty") {
		return errors.New("evidence fact score cannot be negative")
	}
	if len([]rune(f.Detail)) > 256 || strings.ContainsAny(f.Detail, "\r\n") {
		return errors.New("evidence fact detail must be a bounded single line")
	}
	seen := make(map[string]struct{}, len(f.ObservationIDs))
	for _, id := range f.ObservationIDs {
		if strings.TrimSpace(id) == "" || strings.ContainsAny(id, "\r\n") {
			return errors.New("evidence fact observation id must be a non-empty single line")
		}
		if _, ok := seen[id]; ok {
			return fmt.Errorf("duplicate evidence fact observation id %q", id)
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (f EvidenceFact) clone() EvidenceFact {
	f.ObservationIDs = append([]string(nil), f.ObservationIDs...)
	return f
}

func cloneFacts(facts []EvidenceFact) []EvidenceFact {
	if facts == nil {
		return nil
	}
	result := make([]EvidenceFact, len(facts))
	for i, fact := range facts {
		result[i] = fact.clone()
	}
	return result
}

func scoreFacts(facts []EvidenceFact) (int64, int64, error) {
	var support, contradiction, penalty int64
	for _, fact := range facts {
		if err := fact.Validate(); err != nil {
			return 0, 0, err
		}
		switch fact.Side {
		case EvidenceSupport:
			support += fact.Score
		case EvidenceContradiction:
			contradiction += fact.Score
		case EvidenceNeutral:
			if fact.Code == "type.uncertain_penalty" {
				penalty += fact.Score
			}
		}
	}
	if penalty < 0 {
		support += penalty
		contradiction += penalty
	}
	if support < 0 {
		support = 0
	}
	if contradiction < 0 {
		contradiction = 0
	}
	return support, contradiction, nil
}
