package calibrationanalytics

import "synora/internal/cge/calibrationledger"

const SchemaVersion = "calibration-analytics-report-v1"

type AnalyticsInput struct {
	LedgerSnapshot        calibrationledger.Snapshot
	Records               []calibrationledger.CalibrationRecord
	GeneratedFromSequence uint64
	LedgerFingerprint     string
}

type AnalyticsMarkers struct {
	DescriptiveOnly                        bool `json:"descriptive_only"`
	NotModelAccuracy                       bool `json:"not_model_accuracy"`
	NotGroundTruth                         bool `json:"not_ground_truth"`
	NotAutomaticCalibration                bool `json:"not_automatic_calibration"`
	NotAProductionDecision                 bool `json:"not_a_production_decision"`
	NotARecommendation                     bool `json:"not_a_recommendation"`
	NotAuthorization                       bool `json:"not_authorization"`
	NotACommand                            bool `json:"not_a_command"`
	NotAnAction                            bool `json:"not_an_action"`
	NotAnAlert                             bool `json:"not_an_alert"`
	DoesNotChangeThresholds                bool `json:"does_not_change_thresholds"`
	DoesNotChangeWeights                   bool `json:"does_not_change_weights"`
	DoesNotSelectPolicy                    bool `json:"does_not_select_policy"`
	DoesNotDeployPolicy                    bool `json:"does_not_deploy_policy"`
	HistoricalProductionAuthorityUnchanged bool `json:"historical_production_authority_unchanged"`
	NoSecurityMeaning                      bool `json:"no_security_meaning"`
}

type DataSufficiency struct {
	SufficientForGlobalAnalysis   bool     `json:"sufficient_for_global_analysis"`
	SufficientForTrendAnalysis    bool     `json:"sufficient_for_trend_analysis"`
	SufficientForDriftAnalysis    bool     `json:"sufficient_for_drift_analysis"`
	SufficientForPolicyComparison bool     `json:"sufficient_for_policy_comparison"`
	RecordCount                   uint64   `json:"record_count"`
	ComparableRecordCount         uint64   `json:"comparable_record_count"`
	WindowCount                   int      `json:"window_count"`
	EligibleCohortCount           int      `json:"eligible_cohort_count"`
	MissingRecords                uint64   `json:"missing_records"`
	MissingComparableRecords      uint64   `json:"missing_comparable_records"`
	MissingWindows                int      `json:"missing_windows"`
	Limitations                   []string `json:"limitations"`
	Fingerprint                   string   `json:"fingerprint"`
}

type GlobalAnalytics struct {
	RecordCount                       uint64 `json:"record_count"`
	ComparableCount                   uint64 `json:"comparable_count"`
	IncomparableCount                 uint64 `json:"incomparable_count"`
	SignificantDivergenceCount        uint64 `json:"significant_divergence_count"`
	ComparableRatePermille            int    `json:"comparable_rate_permille"`
	SignificantDivergenceRatePermille int    `json:"significant_divergence_rate_permille"`
	AlignmentMeanPermille             int    `json:"alignment_mean_permille"`
	DivergenceMeanPermille            int    `json:"divergence_mean_permille"`
	CoverageMeanPermille              int    `json:"coverage_mean_permille"`
	AlignmentP50Permille              int    `json:"alignment_p50_permille"`
	AlignmentP95Permille              int    `json:"alignment_p95_permille"`
	AlignmentP99Permille              int    `json:"alignment_p99_permille"`
	DivergenceP50Permille             int    `json:"divergence_p50_permille"`
	DivergenceP95Permille             int    `json:"divergence_p95_permille"`
	DivergenceP99Permille             int    `json:"divergence_p99_permille"`
	CoverageP50Permille               int    `json:"coverage_p50_permille"`
	CoverageP95Permille               int    `json:"coverage_p95_permille"`
	CoverageP99Permille               int    `json:"coverage_p99_permille"`
	HistoricalTransitions             uint64 `json:"historical_transitions"`
	CognitiveTransitions              uint64 `json:"cognitive_transitions"`
	AlignedTransitions                uint64 `json:"aligned_transitions"`
	CognitiveMoreConservative         uint64 `json:"cognitive_more_conservative"`
	HistoricalMoreDecisive            uint64 `json:"historical_more_decisive"`
	Fingerprint                       string `json:"fingerprint"`
}

type CategoryAnalytics struct {
	Category                          string `json:"category"`
	RecordCount                       uint64 `json:"record_count"`
	ComparableRatePermille            int    `json:"comparable_rate_permille"`
	SignificantDivergenceRatePermille int    `json:"significant_divergence_rate_permille"`
	AlignmentMeanPermille             int    `json:"alignment_mean_permille"`
	DivergenceMeanPermille            int    `json:"divergence_mean_permille"`
	CoverageMeanPermille              int    `json:"coverage_mean_permille"`
	AlignmentP95Permille              int    `json:"alignment_p95_permille"`
	DivergenceP95Permille             int    `json:"divergence_p95_permille"`
	CoverageP95Permille               int    `json:"coverage_p95_permille"`
	Sufficient                        bool   `json:"sufficient"`
	Fingerprint                       string `json:"fingerprint"`
}

type PolicyCohortKey struct {
	SituationPolicyFingerprint      string `json:"situation_policy_fingerprint"`
	RecommendationPolicyFingerprint string `json:"recommendation_policy_fingerprint"`
	ComparisonPolicyFingerprint     string `json:"comparison_policy_fingerprint"`
}

type PolicyCohortAnalytics struct {
	Key                               PolicyCohortKey `json:"key"`
	RecordCount                       uint64          `json:"record_count"`
	ComparableCount                   uint64          `json:"comparable_count"`
	ComparableRatePermille            int             `json:"comparable_rate_permille"`
	SignificantDivergenceRatePermille int             `json:"significant_divergence_rate_permille"`
	AlignmentMeanPermille             int             `json:"alignment_mean_permille"`
	DivergenceMeanPermille            int             `json:"divergence_mean_permille"`
	CoverageMeanPermille              int             `json:"coverage_mean_permille"`
	AlignmentP95Permille              int             `json:"alignment_p95_permille"`
	DivergenceP95Permille             int             `json:"divergence_p95_permille"`
	CoverageP95Permille               int             `json:"coverage_p95_permille"`
	HistoricalTransitions             uint64          `json:"historical_transitions"`
	CognitiveTransitions              uint64          `json:"cognitive_transitions"`
	CognitiveMoreConservative         uint64          `json:"cognitive_more_conservative"`
	HistoricalMoreDecisive            uint64          `json:"historical_more_decisive"`
	Sufficient                        bool            `json:"sufficient"`
	CohortFingerprint                 string          `json:"cohort_fingerprint"`
}

type WindowAnalytics struct {
	Index                             int    `json:"index"`
	FirstSequence                     uint64 `json:"first_sequence"`
	LastSequence                      uint64 `json:"last_sequence"`
	RecordCount                       uint64 `json:"record_count"`
	ComparableRatePermille            int    `json:"comparable_rate_permille"`
	SignificantDivergenceRatePermille int    `json:"significant_divergence_rate_permille"`
	AlignmentMeanPermille             int    `json:"alignment_mean_permille"`
	DivergenceMeanPermille            int    `json:"divergence_mean_permille"`
	CoverageMeanPermille              int    `json:"coverage_mean_permille"`
	AlignmentP95Permille              int    `json:"alignment_p95_permille"`
	DivergenceP95Permille             int    `json:"divergence_p95_permille"`
	CoverageP95Permille               int    `json:"coverage_p95_permille"`
	WindowFingerprint                 string `json:"window_fingerprint"`
}

type MetricTrend struct {
	Metric             string `json:"metric"`
	FirstValuePermille int    `json:"first_value_permille"`
	LastValuePermille  int    `json:"last_value_permille"`
	DeltaPermille      int    `json:"delta_permille"`
	Direction          string `json:"direction"`
	Significant        bool   `json:"significant"`
	Stable             bool   `json:"stable"`
}

type TrendAnalytics struct {
	Alignment                 MetricTrend `json:"alignment"`
	Divergence                MetricTrend `json:"divergence"`
	Coverage                  MetricTrend `json:"coverage"`
	ComparableRate            MetricTrend `json:"comparable_rate"`
	SignificantDivergenceRate MetricTrend `json:"significant_divergence_rate"`
	WindowCount               int         `json:"window_count"`
	Sufficient                bool        `json:"sufficient"`
	Fingerprint               string      `json:"fingerprint"`
}

type MetricDrift struct {
	Metric               string `json:"metric"`
	BaselineMeanPermille int    `json:"baseline_mean_permille"`
	RecentMeanPermille   int    `json:"recent_mean_permille"`
	MeanDeltaPermille    int    `json:"mean_delta_permille"`
	BaselineP95Permille  int    `json:"baseline_p95_permille"`
	RecentP95Permille    int    `json:"recent_p95_permille"`
	P95DeltaPermille     int    `json:"p95_delta_permille"`
	Detected             bool   `json:"detected"`
	Sufficient           bool   `json:"sufficient"`
}

type DriftAnalytics struct {
	Alignment                 MetricDrift `json:"alignment"`
	Divergence                MetricDrift `json:"divergence"`
	Coverage                  MetricDrift `json:"coverage"`
	ComparableRate            MetricDrift `json:"comparable_rate"`
	SignificantDivergenceRate MetricDrift `json:"significant_divergence_rate"`
	AnyDriftDetected          bool        `json:"any_drift_detected"`
	Sufficient                bool        `json:"sufficient"`
	BaselineRecordCount       uint64      `json:"baseline_record_count"`
	RecentRecordCount         uint64      `json:"recent_record_count"`
	Fingerprint               string      `json:"fingerprint"`
}

type PolicyCohortComparison struct {
	LeftCohortFingerprint                  string `json:"left_cohort_fingerprint"`
	RightCohortFingerprint                 string `json:"right_cohort_fingerprint"`
	AlignmentMeanDeltaPermille             int    `json:"alignment_mean_delta_permille"`
	DivergenceMeanDeltaPermille            int    `json:"divergence_mean_delta_permille"`
	CoverageMeanDeltaPermille              int    `json:"coverage_mean_delta_permille"`
	ComparableRateDeltaPermille            int    `json:"comparable_rate_delta_permille"`
	SignificantDivergenceRateDeltaPermille int    `json:"significant_divergence_rate_delta_permille"`
	LeftRecordCount                        uint64 `json:"left_record_count"`
	RightRecordCount                       uint64 `json:"right_record_count"`
	Sufficient                             bool   `json:"sufficient"`
	Fingerprint                            string `json:"fingerprint"`
}

type PolicyEvaluationMarkers struct {
	ComparativeOnly        bool `json:"comparative_only"`
	NoWinnerSelected       bool `json:"no_winner_selected"`
	NoPolicyRecommended    bool `json:"no_policy_recommended"`
	NoPolicyActivated      bool `json:"no_policy_activated"`
	NoProductionFeedback   bool `json:"no_production_feedback"`
	NoAutomaticCalibration bool `json:"no_automatic_calibration"`
}

func (m AnalyticsMarkers) Validate() error {
	if !allMarkersTrue(m) {
		return ErrInvalidAnalyticsMarkers
	}
	return nil
}

func (m PolicyEvaluationMarkers) Validate() error {
	if !allEvaluationMarkersTrue(m) {
		return ErrInvalidPolicyEvaluationMarkers
	}
	return nil
}

type PolicyEvaluation struct {
	CohortCount                int                      `json:"cohort_count"`
	EligibleCohortCount        int                      `json:"eligible_cohort_count"`
	Comparisons                []PolicyCohortComparison `json:"comparisons"`
	ReferenceCohortFingerprint string                   `json:"reference_cohort_fingerprint"`
	Sufficient                 bool                     `json:"sufficient"`
	Markers                    PolicyEvaluationMarkers  `json:"markers"`
	Fingerprint                string                   `json:"fingerprint"`
}

type CalibrationAnalyticsReport struct {
	SchemaVersion              string                  `json:"schema_version"`
	ReportFingerprint          string                  `json:"report_fingerprint"`
	LedgerFingerprint          string                  `json:"ledger_fingerprint"`
	AnalyticsPolicyFingerprint string                  `json:"analytics_policy_fingerprint"`
	FirstSequence              uint64                  `json:"first_sequence"`
	LastSequence               uint64                  `json:"last_sequence"`
	GeneratedFromSequence      uint64                  `json:"generated_from_sequence"`
	RecordCount                uint64                  `json:"record_count"`
	DataSufficiency            DataSufficiency         `json:"data_sufficiency"`
	Global                     GlobalAnalytics         `json:"global"`
	Categories                 []CategoryAnalytics     `json:"categories"`
	PolicyCohorts              []PolicyCohortAnalytics `json:"policy_cohorts"`
	Windows                    []WindowAnalytics       `json:"windows"`
	Trend                      TrendAnalytics          `json:"trend"`
	Drift                      DriftAnalytics          `json:"drift"`
	PolicyEvaluation           PolicyEvaluation        `json:"policy_evaluation"`
	Markers                    AnalyticsMarkers        `json:"markers"`
}

func (r CalibrationAnalyticsReport) Clone() CalibrationAnalyticsReport {
	r.DataSufficiency.Limitations = append([]string(nil), r.DataSufficiency.Limitations...)
	r.Categories = append([]CategoryAnalytics(nil), r.Categories...)
	r.PolicyCohorts = append([]PolicyCohortAnalytics(nil), r.PolicyCohorts...)
	r.Windows = append([]WindowAnalytics(nil), r.Windows...)
	r.PolicyEvaluation.Comparisons = append([]PolicyCohortComparison(nil), r.PolicyEvaluation.Comparisons...)
	return r
}

func (r CalibrationAnalyticsReport) ValidateMarkers() error {
	if err := r.Markers.Validate(); err != nil {
		return err
	}
	if err := r.PolicyEvaluation.Markers.Validate(); err != nil {
		return err
	}
	return nil
}
