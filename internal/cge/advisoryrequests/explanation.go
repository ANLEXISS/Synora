package advisoryrequests

func Explain(r AdvisoryEvidenceRequest) (AdvisoryExplanation, error) {
	if err := validateRequest(r, DefaultPolicy()); err != nil {
		return AdvisoryExplanation{}, ErrInvalidExplanation
	}
	return AdvisoryExplanation{RequestID: r.ID, RequestKey: r.Key, Status: r.Status, Kind: string(r.Kind), Dimension: string(r.Dimension), SummaryCode: "advisory_evidence_need", CandidateID: string(r.CandidateID), ReasonCodes: uniqueStrings(r.ReasonCodes), RequiredFactCodes: uniqueStrings(r.RequiredFactCodes), HypothesisPairs: uniquePairs(r.HypothesisPairs), DiscriminationPermille: r.DiscriminationPermille, CoverageGainPermille: r.CoverageGainPermille, RedundancyPermille: r.RedundancyPermille, UtilityPermille: r.UtilityPermille, CostClass: r.CostClass, LatencyClass: r.LatencyClass, SensitivityClass: r.SensitivityClass, NotACommand: true, NotAProbability: true, NoSecurityMeaning: true, RequiresExternalMapping: true, RequiresExternalAuthorization: true}, nil
}

func ValidateExplanation(e AdvisoryExplanation) error {
	if e.RequestID == "" || e.RequestKey == "" || e.SummaryCode == "" || e.CandidateID == "" || !e.NotACommand || !e.NotAProbability || !e.NoSecurityMeaning || !e.RequiresExternalMapping || !e.RequiresExternalAuthorization {
		return ErrInvalidExplanation
	}
	return nil
}
