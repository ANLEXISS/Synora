package shadowworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

const qualificationSchemaVersion = "shadow-qualification-sample-v1"

type ProcessSample struct {
	HeapAlloc       uint64 `json:"heap_alloc"`
	HeapInuse       uint64 `json:"heap_inuse"`
	HeapObjects     uint64 `json:"heap_objects"`
	HeapReleased    uint64 `json:"heap_released"`
	Sys             uint64 `json:"sys"`
	TotalAlloc      uint64 `json:"total_alloc"`
	Mallocs         uint64 `json:"mallocs"`
	Frees           uint64 `json:"frees"`
	NumGC           uint32 `json:"num_gc"`
	PauseTotalNS    uint64 `json:"pause_total_ns"`
	LastGCPauseNS   uint64 `json:"last_gc_pause_ns"`
	Goroutines      int    `json:"goroutines"`
	RSSBytes        int64  `json:"rss_bytes,omitempty"`
	CPUUserNS       int64  `json:"cpu_user_ns,omitempty"`
	CPUSystemNS     int64  `json:"cpu_system_ns,omitempty"`
	Threads         int    `json:"threads,omitempty"`
	FileDescriptors int    `json:"file_descriptors,omitempty"`
}

type StorageSample struct {
	StoreMode                   string `json:"store_mode"`
	WALBytes                    int64  `json:"wal_bytes"`
	WALGrowthBytes              int64  `json:"wal_growth_bytes"`
	CheckpointBytes             int64  `json:"checkpoint_bytes"`
	TransactionsSinceCheckpoint uint64 `json:"transactions_since_checkpoint"`
	LastCheckpointDurationNS    uint64 `json:"last_checkpoint_duration_ns"`
	LastRecoveryDurationNS      uint64 `json:"last_recovery_duration_ns"`
	AverageTransactionBytes     int64  `json:"average_transaction_bytes"`
	MaximumTransactionBytes     int64  `json:"maximum_transaction_bytes"`
	StorageLimitReached         bool   `json:"storage_limit_reached"`
}

type StageSample struct {
	Stage           string `json:"stage"`
	Count           uint64 `json:"count"`
	Successes       uint64 `json:"successes"`
	Failures        uint64 `json:"failures"`
	Timeouts        uint64 `json:"timeouts"`
	TotalDurationNS uint64 `json:"total_duration_ns"`
	MinDurationNS   uint64 `json:"min_duration_ns"`
	MaxDurationNS   uint64 `json:"max_duration_ns"`
	RecentP50NS     uint64 `json:"recent_p50_ns"`
	RecentP95NS     uint64 `json:"recent_p95_ns"`
	RecentP99NS     uint64 `json:"recent_p99_ns"`
}

type HistoricalIsolationSample struct {
	DecisionComparisons                uint64 `json:"decision_comparisons"`
	DecisionMismatches                 uint64 `json:"decision_mismatches"`
	HistoricalFingerprintComparisons   uint64 `json:"historical_fingerprint_comparisons"`
	HistoricalFingerprintMismatches    uint64 `json:"historical_fingerprint_mismatches"`
	TrySubmitFailuresObserved          uint64 `json:"try_submit_failures_observed"`
	TrySubmitFailuresAffectingDecision uint64 `json:"try_submit_failures_affecting_decision"`
}

type QualificationSample struct {
	SchemaVersion                                         string                    `json:"schema_version"`
	RunID                                                 string                    `json:"run_id"`
	Profile                                               QualificationProfile      `json:"profile"`
	SampleSequence                                        uint64                    `json:"sample_sequence"`
	SampledAt                                             time.Time                 `json:"sampled_at"`
	Uptime                                                time.Duration             `json:"uptime"`
	RuntimeState                                          string                    `json:"runtime_state"`
	PipelineDepth                                         string                    `json:"pipeline_depth"`
	CircuitState                                          string                    `json:"circuit_state"`
	WorkflowRevision                                      uint64                    `json:"workflow_revision"`
	LastSequence                                          uint64                    `json:"last_sequence"`
	QueueDepth                                            int                       `json:"queue_depth"`
	QueueCapacity                                         int                       `json:"queue_capacity"`
	QueueHighWaterMark                                    int                       `json:"queue_high_water_mark"`
	EpisodeCount                                          int                       `json:"episode_count"`
	Received                                              uint64                    `json:"received"`
	Accepted                                              uint64                    `json:"accepted"`
	Rejected                                              uint64                    `json:"rejected"`
	DroppedQueueFull                                      uint64                    `json:"dropped_queue_full"`
	Duplicates                                            uint64                    `json:"duplicates"`
	CyclesSucceeded                                       uint64                    `json:"cycles_succeeded"`
	CyclesFailed                                          uint64                    `json:"cycles_failed"`
	CyclesTimedOut                                        uint64                    `json:"cycles_timed_out"`
	CommitsSucceeded                                      uint64                    `json:"commits_succeeded"`
	CommitsFailed                                         uint64                    `json:"commits_failed"`
	CheckpointsSucceeded                                  uint64                    `json:"checkpoints_succeeded"`
	CheckpointsFailed                                     uint64                    `json:"checkpoints_failed"`
	CalibrationLedgerRecords                              uint64                    `json:"calibration_ledger_records"`
	CalibrationLedgerBytes                                int64                     `json:"calibration_ledger_bytes"`
	CalibrationLedgerAppendFailures                       uint64                    `json:"calibration_ledger_append_failures"`
	CalibrationLedgerIntegrityFailures                    uint64                    `json:"calibration_ledger_integrity_failures"`
	CalibrationAlignmentMeanPermille                      int                       `json:"calibration_alignment_mean_permille"`
	CalibrationDivergenceMeanPermille                     int                       `json:"calibration_divergence_mean_permille"`
	CalibrationCoverageMeanPermille                       int                       `json:"calibration_coverage_mean_permille"`
	CalibrationComparableRatePermille                     int                       `json:"calibration_comparable_rate_permille"`
	CalibrationSignificantDivergenceRatePermille          int                       `json:"calibration_significant_divergence_rate_permille"`
	CalibrationAnalyticsAvailable                         bool                      `json:"calibration_analytics_available"`
	CalibrationAnalyticsSufficient                        bool                      `json:"calibration_analytics_sufficient"`
	CalibrationAnalyticsRecordCount                       uint64                    `json:"calibration_analytics_record_count"`
	CalibrationAnalyticsWindowCount                       int                       `json:"calibration_analytics_window_count"`
	CalibrationAnalyticsEligibleCohortCount               int                       `json:"calibration_analytics_eligible_cohort_count"`
	CalibrationAnalyticsAlignmentMeanPermille             int                       `json:"calibration_analytics_alignment_mean_permille"`
	CalibrationAnalyticsDivergenceMeanPermille            int                       `json:"calibration_analytics_divergence_mean_permille"`
	CalibrationAnalyticsCoverageMeanPermille              int                       `json:"calibration_analytics_coverage_mean_permille"`
	CalibrationAnalyticsComparableRatePermille            int                       `json:"calibration_analytics_comparable_rate_permille"`
	CalibrationAnalyticsSignificantDivergenceRatePermille int                       `json:"calibration_analytics_significant_divergence_rate_permille"`
	CalibrationAnalyticsAnyDriftDetected                  bool                      `json:"calibration_analytics_any_drift_detected"`
	InvalidLineage                                        uint64                    `json:"invalid_lineage"`
	RecoveryDigestMismatches                              uint64                    `json:"recovery_digest_mismatches"`
	Process                                               ProcessSample             `json:"process"`
	Storage                                               StorageSample             `json:"storage"`
	StageCounters                                         []StageSample             `json:"stage_counters"`
	HistoricalIsolation                                   HistoricalIsolationSample `json:"historical_isolation"`
	LastErrorCode                                         string                    `json:"last_error_code,omitempty"`
	Fingerprint                                           string                    `json:"fingerprint"`
}

func (s QualificationSample) Clone() QualificationSample {
	s.StageCounters = append([]StageSample(nil), s.StageCounters...)
	return s
}

func QualificationSampleFingerprint(sample QualificationSample) string {
	copy := sample.Clone()
	copy.Fingerprint = ""
	encoded, _ := json.Marshal(copy)
	digest := sha256.Sum256(encoded)
	return "shadow-qualification-sample-v1:" + hex.EncodeToString(digest[:])
}

func validQualificationSample(sample QualificationSample) bool {
	return sample.SchemaVersion == qualificationSchemaVersion && sample.RunID != "" && sample.SampleSequence > 0 && !sample.SampledAt.IsZero() && sample.Fingerprint == QualificationSampleFingerprint(sample)
}

func canonicalStageSamples(values []StageSample) []StageSample {
	out := append([]StageSample(nil), values...)
	sort.Slice(out, func(i, j int) bool { return out[i].Stage < out[j].Stage })
	return out
}
