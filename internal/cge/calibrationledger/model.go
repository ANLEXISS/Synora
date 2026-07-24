package calibrationledger

const (
	RecordSchemaVersion   = "calibration-ledger-record-v1"
	EnvelopeSchemaVersion = "calibration-ledger-envelope-v1"
	GenesisVersion        = "calibration-ledger-genesis-v1"
	PolicySchemaVersion   = "calibration-ledger-policy-v1"
	SummarySchemaVersion  = "calibration-ledger-summary-v1"
)

type CalibrationRecord struct {
	SchemaVersion                   string                        `json:"schema_version"`
	RecordID                        string                        `json:"record_id"`
	Sequence                        uint64                        `json:"sequence"`
	ComparisonFingerprint           string                        `json:"comparison_fingerprint"`
	PreviousRecordFingerprint       string                        `json:"previous_record_fingerprint,omitempty"`
	Category                        string                        `json:"category"`
	Comparable                      bool                          `json:"comparable"`
	SignificantDivergence           bool                          `json:"significant_divergence"`
	AlignmentPermille               int                           `json:"alignment_permille"`
	DivergencePermille              int                           `json:"divergence_permille"`
	CoveragePermille                int                           `json:"coverage_permille"`
	HistoricalStateChanged          bool                          `json:"historical_state_changed"`
	CognitiveTransitionFound        bool                          `json:"cognitive_transition_found"`
	HistoricalMoreDecisive          bool                          `json:"historical_more_decisive"`
	CognitiveMoreConservative       bool                          `json:"cognitive_more_conservative"`
	HistoricalDecisionRevision      uint64                        `json:"historical_decision_revision,omitempty"`
	CognitiveSituationRevision      uint64                        `json:"cognitive_situation_revision,omitempty"`
	RecommendationSetRevision       uint64                        `json:"recommendation_set_revision,omitempty"`
	ComparisonRevision              uint64                        `json:"comparison_revision,omitempty"`
	SituationPolicyFingerprint      string                        `json:"situation_policy_fingerprint,omitempty"`
	RecommendationPolicyFingerprint string                        `json:"recommendation_policy_fingerprint,omitempty"`
	ComparisonPolicyFingerprint     string                        `json:"comparison_policy_fingerprint,omitempty"`
	Dimensions                      []CalibrationDimensionSummary `json:"dimensions,omitempty"`
	SourceDecisionFingerprint       string                        `json:"source_decision_fingerprint,omitempty"`
	SourceSituationFingerprint      string                        `json:"source_situation_fingerprint,omitempty"`
	SourceRecommendationFingerprint string                        `json:"source_recommendation_fingerprint,omitempty"`
	SourceObservedAtUnixNano        int64                         `json:"source_observed_at_unix_nano,omitempty"`
	SourceDecidedAtUnixNano         int64                         `json:"source_decided_at_unix_nano,omitempty"`
	RecordFingerprint               string                        `json:"record_fingerprint"`
	Markers                         CalibrationRecordMarkers      `json:"markers"`
}

type CalibrationRecordMarkers struct {
	HistoricalDecisionRetainsAuthority bool `json:"historical_decision_retains_authority"`
	NotAProductionDecision             bool `json:"not_a_production_decision"`
	NotARecommendation                 bool `json:"not_a_recommendation"`
	NotAuthorization                   bool `json:"not_authorization"`
	NotACommand                        bool `json:"not_a_command"`
	NotAnAction                        bool `json:"not_an_action"`
	NotAnAlert                         bool `json:"not_an_alert"`
	DoesNotUpdateThresholds            bool `json:"does_not_update_thresholds"`
	DoesNotUpdateWeights               bool `json:"does_not_update_weights"`
	DoesNotTrainAutomatically          bool `json:"does_not_train_automatically"`
	DoesNotOverrideHistoricalDecision  bool `json:"does_not_override_historical_decision"`
	CalibrationOnly                    bool `json:"calibration_only"`
	NoSecurityMeaning                  bool `json:"no_security_meaning"`
}

type CalibrationDimensionSummary struct {
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	Comparable         bool   `json:"comparable"`
	AlignmentPermille  int    `json:"alignment_permille"`
	DivergencePermille int    `json:"divergence_permille"`
	CoveragePermille   int    `json:"coverage_permille"`
	Fingerprint        string `json:"fingerprint"`
}

type JournalEnvelope struct {
	SchemaVersion        string            `json:"schema_version"`
	Sequence             uint64            `json:"sequence"`
	PreviousEnvelopeHash string            `json:"previous_envelope_hash"`
	Record               CalibrationRecord `json:"record"`
	RecordHash           string            `json:"record_hash"`
	EnvelopeHash         string            `json:"envelope_hash"`
}

type AppendResult struct {
	Appended            bool
	Duplicate           bool
	Sequence            uint64
	RecordFingerprint   string
	EnvelopeFingerprint string
}

type RecoveryResult struct {
	Completed               bool
	RepairedTrailingRecord  bool
	RecordCount             uint64
	LastSequence            uint64
	LastRecordFingerprint   string
	LastEnvelopeFingerprint string
	Bytes                   int64
}

type Snapshot struct {
	SchemaVersion              string            `json:"schema_version"`
	RecordCount                uint64            `json:"record_count"`
	FirstSequence              uint64            `json:"first_sequence"`
	LastSequence               uint64            `json:"last_sequence"`
	LastRecordFingerprint      string            `json:"last_record_fingerprint,omitempty"`
	LastEnvelopeFingerprint    string            `json:"last_envelope_fingerprint,omitempty"`
	CategoryCounts             map[string]uint64 `json:"category_counts"`
	ComparableCount            uint64            `json:"comparable_count"`
	SignificantDivergenceCount uint64            `json:"significant_divergence_count"`
	IncomparableCount          uint64            `json:"incomparable_count"`
	StaleCount                 uint64            `json:"stale_count"`
	InvalidatedCount           uint64            `json:"invalidated_count"`
	LedgerBytes                int64             `json:"ledger_bytes"`
	Aggregate                  AggregateSnapshot `json:"aggregate"`
	Digest                     string            `json:"digest"`
}

type Query struct {
	SequenceFrom          uint64
	SequenceTo            uint64
	Categories            []string
	Comparable            *bool
	SignificantDivergence *bool
	Limit                 int
}

type QueryResult struct {
	Records []CalibrationRecord
	Matched uint64
}

type CalibrationSummary struct {
	LedgerFingerprint                 string                    `json:"ledger_fingerprint"`
	RecordCount                       uint64                    `json:"record_count"`
	ComparableRatePermille            int                       `json:"comparable_rate_permille"`
	SignificantDivergenceRatePermille int                       `json:"significant_divergence_rate_permille"`
	AlignmentMeanPermille             int                       `json:"alignment_mean_permille"`
	DivergenceMeanPermille            int                       `json:"divergence_mean_permille"`
	CoverageMeanPermille              int                       `json:"coverage_mean_permille"`
	HistoricalTransitionOnlyCount     uint64                    `json:"historical_transition_only_count"`
	CognitiveTransitionOnlyCount      uint64                    `json:"cognitive_transition_only_count"`
	CognitiveMoreConservativeCount    uint64                    `json:"cognitive_more_conservative_count"`
	HistoricalMoreDecisiveCount       uint64                    `json:"historical_more_decisive_count"`
	Markers                           CalibrationSummaryMarkers `json:"markers"`
}

type CalibrationSummaryMarkers struct {
	DescriptiveOnly         bool `json:"descriptive_only"`
	NotModelAccuracy        bool `json:"not_model_accuracy"`
	NotAutomaticCalibration bool `json:"not_automatic_calibration"`
	NotAProductionDecision  bool `json:"not_a_production_decision"`
	NotAnAlert              bool `json:"not_an_alert"`
	NoSecurityMeaning       bool `json:"no_security_meaning"`
}
