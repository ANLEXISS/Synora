package cognitivesituation

import "synora/internal/cge/durableworkflow"

type CognitiveSituationReadiness struct {
	ConsolidationImplemented           bool
	KnowledgeSummaryImplemented        bool
	HypothesisSummaryImplemented       bool
	EvidenceSummaryImplemented         bool
	AdvisorySummaryImplemented         bool
	CapabilitySummaryImplemented       bool
	AuthorizationSummaryImplemented    bool
	FreshnessValidated                 bool
	AmbiguityPreserved                 bool
	MissingInformationPreserved        bool
	SourceLineageValidated             bool
	RecommendationReadinessImplemented bool
	ComparisonImplemented              bool
	ExplanationImplemented             bool
	Deterministic                      bool
	DefensiveSnapshotsValidated        bool
	RecoveryRebuildValidated           bool
	ShadowRuntimeIntegrated            bool
	ConcurrencyValidated               bool
	ProductionDecisionIntegrated       bool
	RecommendationEngineImplemented    bool
	ActionExecutionImplemented         bool
	SecurityAuthority                  bool
	ReadyForRecommendationPlanning     bool
	Limitations                        []string
}

func Readiness() CognitiveSituationReadiness {
	return CognitiveSituationReadiness{
		ConsolidationImplemented:           true,
		KnowledgeSummaryImplemented:        true,
		HypothesisSummaryImplemented:       true,
		EvidenceSummaryImplemented:         true,
		AdvisorySummaryImplemented:         true,
		CapabilitySummaryImplemented:       true,
		AuthorizationSummaryImplemented:    true,
		FreshnessValidated:                 true,
		AmbiguityPreserved:                 true,
		MissingInformationPreserved:        true,
		SourceLineageValidated:             true,
		RecommendationReadinessImplemented: true,
		ComparisonImplemented:              true,
		ExplanationImplemented:             true,
		Deterministic:                      true,
		DefensiveSnapshotsValidated:        true,
		RecoveryRebuildValidated:           true,
		ShadowRuntimeIntegrated:            true,
		ConcurrencyValidated:               true,
		ProductionDecisionIntegrated:       false,
		RecommendationEngineImplemented:    false,
		ActionExecutionImplemented:         false,
		SecurityAuthority:                  false,
		ReadyForRecommendationPlanning:     true,
		Limitations: []string{
			"Derived in memory; no separate WAL or checkpoint.",
			"No recommendation engine, authorization grant, command or action exists.",
		},
	}
}

func layerExpected(depth ExpectedPipelineDepth, layer durableworkflow.LayerKind) bool {
	return expectedLayer(depth, layer)
}
