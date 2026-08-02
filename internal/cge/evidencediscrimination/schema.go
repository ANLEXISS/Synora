package evidencediscrimination

import (
	"sort"

	"synora/internal/cge/situationfacts"
	"synora/internal/cge/situationhypotheses"
)

func ValidateCatalog(c EvidenceCatalog) error {
	if c.Version == "" || len(c.Definitions) == 0 {
		return ErrInvalidCatalog
	}
	last := EvidenceCandidateKind("")
	seen := map[EvidenceCandidateKind]struct{}{}
	hschema := situationhypotheses.Schema()
	if err := hschema.Validate(); err != nil {
		return ErrInvalidCatalog
	}
	for _, d := range c.Definitions {
		if d.Kind == "" || d.Kind <= last || forbiddenCandidateTerm(string(d.Kind)) || d.Description == "" || forbiddenCandidateTerm(d.Description) || !validCandidateKind(d.Kind) || d.Dimension == "" || !validCost(d.DefaultCostClass) || !validLatency(d.DefaultLatencyClass) || !validSensitivity(d.DefaultSensitivityClass) {
			return ErrInvalidDefinition
		}
		if _, ok := seen[d.Kind]; ok {
			return ErrInvalidDefinition
		}
		seen[d.Kind] = struct{}{}
		factSeen := map[situationfacts.FactCode]struct{}{}
		for _, code := range d.RequiredFactCodes {
			if !validFactCode(code) {
				return ErrUnknownFactCode
			}
			if _, ok := factSeen[code]; ok {
				return ErrInvalidDefinition
			}
			factSeen[code] = struct{}{}
		}
		kindSeen := map[situationhypotheses.HypothesisKind]struct{}{}
		for _, kind := range d.ApplicableHypothesisKinds {
			if _, ok := hschema.Definition(kind); !ok {
				return ErrUnknownHypothesisKind
			}
			if _, ok := kindSeen[kind]; ok {
				return ErrInvalidDefinition
			}
			kindSeen[kind] = struct{}{}
		}
		if len(d.Outcomes) == 0 {
			return ErrInvalidDefinition
		}
		outSeen := map[string]struct{}{}
		for _, o := range d.Outcomes {
			if err := validateOutcomeDefinition(o, outSeen, hschema); err != nil {
				return err
			}
			if _, ok := factSeen[o.FactCode]; !ok {
				return ErrInvalidDefinition
			}
		}
		last = d.Kind
	}
	return nil
}

func validateOutcomeDefinition(o OutcomeDefinition, seen map[string]struct{}, schema situationhypotheses.HypothesisSchema) error {
	if o.ID == "" || o.FactCode == "" || o.DescriptionCode == "" || forbiddenCandidateTerm(o.ID) || forbiddenCandidateTerm(o.DescriptionCode) || !validOutcomeOperator(o.Operator) {
		return ErrInvalidOutcome
	}
	if _, ok := seen[o.ID]; ok {
		return ErrOutcomeIDCollision
	}
	seen[o.ID] = struct{}{}
	def, ok := situationfacts.Schema().Definition(o.FactCode)
	if !ok {
		return ErrUnknownFactCode
	}
	if o.Value != nil {
		if err := o.Value.Validate(256, 256); err != nil || o.Value.Kind != def.ValueKind {
			return ErrInvalidOutcome
		}
	}
	if (o.Operator == OutcomeValueEquals || o.Operator == OutcomeValueNotEquals || o.Operator == OutcomeValueGreaterThan || o.Operator == OutcomeValueLessThan) && o.Value == nil {
		return ErrInvalidOutcome
	}
	if (o.Operator == OutcomeFactPresent || o.Operator == OutcomeFactAbsent || o.Operator == OutcomeConflictPresent || o.Operator == OutcomeConflictAbsent) && o.Value != nil {
		return ErrInvalidOutcome
	}
	for _, kinds := range [][]situationhypotheses.HypothesisKind{o.Supports, o.Contradicts, o.ReducesMissingFor} {
		for _, kind := range kinds {
			if _, ok := schema.Definition(kind); !ok {
				return ErrUnknownHypothesisKind
			}
		}
	}
	return nil
}

func validCandidateKind(k EvidenceCandidateKind) bool {
	for _, value := range allCandidateKinds() {
		if value == k {
			return true
		}
	}
	return false
}
func validOutcomeOperator(o OutcomeOperator) bool {
	switch o {
	case OutcomeFactPresent, OutcomeFactAbsent, OutcomeValueEquals, OutcomeValueNotEquals, OutcomeValueGreaterThan, OutcomeValueLessThan, OutcomeConflictPresent, OutcomeConflictAbsent:
		return true
	}
	return false
}

func sortCatalog(c EvidenceCatalog) EvidenceCatalog {
	out := c
	out.Definitions = append([]EvidenceCandidateDefinition(nil), c.Definitions...)
	sort.Slice(out.Definitions, func(i, j int) bool { return out.Definitions[i].Kind < out.Definitions[j].Kind })
	return out
}
