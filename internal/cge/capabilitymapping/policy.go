package capabilitymapping

type Policy struct {
	MaxCandidatesPerRequest     int
	MaxStoredMappingsPerRequest int

	MinCompatibilityPermille   int
	MinQualityPermille         int
	MinUtilityPermille         int
	MinPreferredMarginPermille int

	AllowDegradedCapabilities bool
	AllowUnknownStatus        bool
	AllowUnknownScope         bool
	RequireCalibratedQuality  bool
	AnalyzeSuppressedRequests bool

	MaximumCostClass        CapabilityCostClass
	MaximumLatencyClass     CapabilityLatencyClass
	MaximumSensitivityClass CapabilitySensitivityClass

	CompatibilityWeightPermille int
	QualityWeightPermille       int
	ConstraintWeightPermille    int
	ScopeWeightPermille         int
	AvailabilityWeightPermille  int

	CostPenaltyWeightPermille        int
	LatencyPenaltyWeightPermille     int
	SensitivityPenaltyWeightPermille int

	PreserveIncompatibleCandidates bool
}

func DefaultPolicy() Policy {
	return Policy{
		MaxCandidatesPerRequest: 16, MaxStoredMappingsPerRequest: 32,
		MinCompatibilityPermille: 600, MinQualityPermille: 400, MinUtilityPermille: 350, MinPreferredMarginPermille: 75,
		AllowDegradedCapabilities: true, AllowUnknownStatus: false, AllowUnknownScope: true, RequireCalibratedQuality: false, AnalyzeSuppressedRequests: false,
		MaximumCostClass: CapabilityCostHigh, MaximumLatencyClass: CapabilityLatencyExtended, MaximumSensitivityClass: CapabilitySensitivityModerate,
		CompatibilityWeightPermille: 350, QualityWeightPermille: 250, ConstraintWeightPermille: 150, ScopeWeightPermille: 150, AvailabilityWeightPermille: 100,
		CostPenaltyWeightPermille: 250, LatencyPenaltyWeightPermille: 250, SensitivityPenaltyWeightPermille: 500,
		PreserveIncompatibleCandidates: true,
	}
}

func (p Policy) Validate() error {
	if p.MaxCandidatesPerRequest <= 0 || p.MaxStoredMappingsPerRequest < p.MaxCandidatesPerRequest || p.MinCompatibilityPermille < 0 || p.MinCompatibilityPermille > 1000 || p.MinQualityPermille < 0 || p.MinQualityPermille > 1000 || p.MinUtilityPermille < 0 || p.MinUtilityPermille > 1000 || p.MinPreferredMarginPermille < 0 || p.MinPreferredMarginPermille > 1000 {
		return ErrInvalidPolicy
	}
	if !validClass(string(p.MaximumCostClass), classCost) || !validClass(string(p.MaximumLatencyClass), classLatency) || !validClass(string(p.MaximumSensitivityClass), classSensitivity) {
		return ErrInvalidPolicy
	}
	positive := p.CompatibilityWeightPermille + p.QualityWeightPermille + p.ConstraintWeightPermille + p.ScopeWeightPermille + p.AvailabilityWeightPermille
	penalty := p.CostPenaltyWeightPermille + p.LatencyPenaltyWeightPermille + p.SensitivityPenaltyWeightPermille
	if positive != 1000 || penalty != 1000 {
		return ErrInvalidPolicy
	}
	for _, weight := range []int{p.CompatibilityWeightPermille, p.QualityWeightPermille, p.ConstraintWeightPermille, p.ScopeWeightPermille, p.AvailabilityWeightPermille, p.CostPenaltyWeightPermille, p.LatencyPenaltyWeightPermille, p.SensitivityPenaltyWeightPermille} {
		if weight < 0 || weight > 1000 {
			return ErrInvalidPolicy
		}
	}
	return nil
}

func (p Policy) Fingerprint() string {
	return digestJSON("capability-mapping-policy-v1:", struct {
		MaxCandidates, MaxStored, MinCompatibility, MinQuality, MinUtility, MinMargin                                    int
		AllowDegraded, AllowUnknownStatus, AllowUnknownScope, RequireCalibrated, AnalyzeSuppressed, PreserveIncompatible bool
		MaximumCost, MaximumLatency, MaximumSensitivity                                                                  string
		CompatibilityWeight, QualityWeight, ConstraintWeight, ScopeWeight, AvailabilityWeight                            int
		CostPenalty, LatencyPenalty, SensitivityPenalty                                                                  int
	}{p.MaxCandidatesPerRequest, p.MaxStoredMappingsPerRequest, p.MinCompatibilityPermille, p.MinQualityPermille, p.MinUtilityPermille, p.MinPreferredMarginPermille, p.AllowDegradedCapabilities, p.AllowUnknownStatus, p.AllowUnknownScope, p.RequireCalibratedQuality, p.AnalyzeSuppressedRequests, p.PreserveIncompatibleCandidates, string(p.MaximumCostClass), string(p.MaximumLatencyClass), string(p.MaximumSensitivityClass), p.CompatibilityWeightPermille, p.QualityWeightPermille, p.ConstraintWeightPermille, p.ScopeWeightPermille, p.AvailabilityWeightPermille, p.CostPenaltyWeightPermille, p.LatencyPenaltyWeightPermille, p.SensitivityPenaltyWeightPermille})
}
