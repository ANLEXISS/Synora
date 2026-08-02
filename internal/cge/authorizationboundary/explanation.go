package authorizationboundary

import "synora/internal/cge/capabilitymapping"

type AuthorizationExplanation struct {
	CandidateID          string
	CapabilityInstanceID capabilitymapping.CapabilityInstanceID
	CapabilityKind       capabilitymapping.CapabilityKind

	Status   AuthorizationEligibilityStatus
	Eligible bool

	SummaryCode string

	AppliedRuleIDs      []string
	DenyingRuleIDs      []string
	ConfirmationRuleIDs []string
	SatisfiedGrantIDs   []ExternalGrantID
	MissingGrantKinds   []ExternalGrantKind

	SatisfiedConditions []string
	MissingConditions   []string
	ViolatedConditions  []string
	ReasonCodes         []string

	PolicyCoveragePermille int
	GrantCoveragePermille  int
	ScopeCoveragePermille  int
	EligibilityPermille    int

	NotAnExecutionGrant           bool
	NotACommand                   bool
	NotAReservation               bool
	NotAProbability               bool
	NoSecurityMeaning             bool
	RequiresSeparateGrantIssuance bool
}

func Explain(candidate AuthorizationCandidateAssessment) (AuthorizationExplanation, error) {
	if candidate.ID == "" || candidate.Fingerprint == "" || candidateFingerprint(candidate) != candidate.Fingerprint {
		return AuthorizationExplanation{}, ErrInvalidExplanation
	}
	return AuthorizationExplanation{CandidateID: candidate.ID, CapabilityInstanceID: candidate.CapabilityInstanceID, CapabilityKind: candidate.CapabilityKind, Status: candidate.Status, Eligible: candidate.Eligible, SummaryCode: "authorization." + string(candidate.Status), AppliedRuleIDs: append([]string(nil), candidate.AppliedRuleIDs...), DenyingRuleIDs: append([]string(nil), candidate.DenyingRuleIDs...), ConfirmationRuleIDs: append([]string(nil), candidate.ConfirmationRuleIDs...), SatisfiedGrantIDs: append([]ExternalGrantID(nil), candidate.SatisfiedGrantIDs...), MissingGrantKinds: append([]ExternalGrantKind(nil), candidate.MissingGrantKinds...), SatisfiedConditions: append([]string(nil), candidate.SatisfiedConditions...), MissingConditions: append([]string(nil), candidate.MissingConditions...), ViolatedConditions: append([]string(nil), candidate.ViolatedConditions...), ReasonCodes: append([]string(nil), candidate.ReasonCodes...), PolicyCoveragePermille: candidate.PolicyCoveragePermille, GrantCoveragePermille: candidate.GrantCoveragePermille, ScopeCoveragePermille: candidate.ScopeCoveragePermille, EligibilityPermille: candidate.EligibilityPermille, NotAnExecutionGrant: true, NotACommand: true, NotAReservation: true, NotAProbability: true, NoSecurityMeaning: true, RequiresSeparateGrantIssuance: true}, nil
}

func ValidateExplanation(explanation AuthorizationExplanation) error {
	if explanation.CandidateID == "" || !validEligibilityStatus(explanation.Status) || !explanation.NotAnExecutionGrant || !explanation.NotACommand || !explanation.NotAReservation || !explanation.NotAProbability || !explanation.NoSecurityMeaning || !explanation.RequiresSeparateGrantIssuance {
		return ErrInvalidExplanation
	}
	for _, value := range []int{explanation.PolicyCoveragePermille, explanation.GrantCoveragePermille, explanation.ScopeCoveragePermille, explanation.EligibilityPermille} {
		if value < 0 || value > 1000 {
			return ErrInvalidExplanation
		}
	}
	return nil
}
