// Package campaign contains development-only, deterministic long-running
// Shadow experiments. Labels are analysis metadata and never enter CGE.
package campaign

import (
	"time"

	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/deviation"
)

type ScheduleTemplate struct {
	StartMinuteOfDay int `json:"start_minute_of_day"`
	EndMinuteOfDay   int `json:"end_minute_of_day"`
}

type VariationPolicy struct {
	ScheduleJitterMinutes   int `json:"schedule_jitter_minutes"`
	SkipProbabilityPermille int `json:"skip_probability_permille"`
}

type ResidentProfile struct {
	ID              string           `json:"id"`
	WeekdaySchedule ScheduleTemplate `json:"weekday_schedule"`
	WeekendSchedule ScheduleTemplate `json:"weekend_schedule"`
	Variation       VariationPolicy  `json:"variation"`
}

type NodeProfile struct {
	ID         string              `json:"id"`
	ZoneID     string              `json:"zone_id,omitempty"`
	Kind       cgecontext.NodeKind `json:"kind"`
	EntryPoint bool                `json:"entry_point,omitempty"`
	Exterior   bool                `json:"exterior,omitempty"`
}

type RoutineTemplate struct {
	ID                  string                    `json:"id"`
	ResidentID          string                    `json:"resident_id"`
	DaysOfWeek          []time.Weekday            `json:"days_of_week"`
	StartMinuteOfDay    int                       `json:"start_minute_of_day"`
	VariationMinutes    int                       `json:"variation_minutes"`
	Path                []string                  `json:"path"`
	HouseMode           cgecontext.HouseMode      `json:"house_mode"`
	Occupancy           cgecontext.OccupancyState `json:"occupancy"`
	ProbabilityPermille int                       `json:"probability_permille"`
}

type EpisodeLabel string

const (
	LabelOrdinary            EpisodeLabel = "ordinary"
	LabelBenignVariation     EpisodeLabel = "benign_variation"
	LabelRoutineChange       EpisodeLabel = "routine_change"
	LabelRareLegitimate      EpisodeLabel = "rare_but_legitimate"
	LabelSyntheticIntrusion  EpisodeLabel = "synthetic_intrusion"
	LabelSensorDropout       EpisodeLabel = "sensor_dropout"
	LabelIdentityUncertain   EpisodeLabel = "identity_uncertain"
	LabelTopologyUnavailable EpisodeLabel = "topology_unavailable"
	LabelSystemRestart       EpisodeLabel = "system_restart"
)

type EpisodeTemplate struct {
	ID               string        `json:"id"`
	Label            EpisodeLabel  `json:"label"`
	StartDay         int           `json:"start_day"`
	StartMinuteOfDay int           `json:"start_minute_of_day"`
	Duration         time.Duration `json:"duration"`
	ResidentID       string        `json:"resident_id,omitempty"`
	Path             []string      `json:"path"`
	RepeatEveryDays  int           `json:"repeat_every_days,omitempty"`
	RepeatCount      int           `json:"repeat_count,omitempty"`
}

type RestartPolicy struct {
	EveryDays int `json:"every_days"`
}

type CheckpointPolicy struct {
	EveryDays     int  `json:"every_days"`
	BeforeRestart bool `json:"before_restart"`
}

type Profile struct {
	ID               string            `json:"id"`
	Description      string            `json:"description"`
	StartAt          time.Time         `json:"start_at"`
	Timezone         string            `json:"timezone"`
	DurationDays     int               `json:"duration_days"`
	Seed             uint64            `json:"seed"`
	Residents        []ResidentProfile `json:"residents"`
	Nodes            []NodeProfile     `json:"nodes"`
	RoutineTemplates []RoutineTemplate `json:"routine_templates"`
	Episodes         []EpisodeTemplate `json:"episodes"`
	RestartPolicy    RestartPolicy     `json:"restart_policy"`
	CheckpointPolicy CheckpointPolicy  `json:"checkpoint_policy"`
}

type Timeline struct {
	ProfileID string          `json:"profile_id"`
	Seed      uint64          `json:"seed"`
	StartAt   time.Time       `json:"start_at"`
	EndAt     time.Time       `json:"end_at"`
	Events    []TimelineEvent `json:"events"`
}

type TimelineEvent struct {
	ID                string                    `json:"id"`
	OccurredAt        time.Time                 `json:"occurred_at"`
	ResidentID        string                    `json:"resident_id,omitempty"`
	NodeID            string                    `json:"node_id"`
	Label             EpisodeLabel              `json:"label"`
	EpisodeID         string                    `json:"episode_id"`
	ContextQuality    cgecontext.ContextQuality `json:"context_quality"`
	ContextAvailable  bool                      `json:"context_available"`
	TopologyAvailable bool                      `json:"topology_available"`
	RestartBefore     bool                      `json:"restart_before"`
	CheckpointAfter   bool                      `json:"checkpoint_after"`
}

type ConfigurationSnapshot struct {
	Timezone                 string           `json:"timezone"`
	ContextEnabled           bool             `json:"context_enabled"`
	ContextAllowPartial      bool             `json:"context_allow_partial"`
	RoutineLearningEnabled   bool             `json:"routine_learning_enabled"`
	RoutinePolicyVersion     string           `json:"routine_policy_version"`
	DeviationEnabled         bool             `json:"deviation_enabled"`
	DeviationPolicy          deviation.Policy `json:"deviation_policy"`
	DeviationRecentLimit     int              `json:"deviation_recent_limit"`
	DeviationMaxAssessments  int              `json:"deviation_max_assessments"`
	CognitiveEnabled         bool             `json:"cognitive_enabled"`
	AssociationPolicyVersion string           `json:"association_policy_version"`
	EvidencePolicyVersion    string           `json:"evidence_policy_version"`
}

type EventResult struct {
	EventID                  string                     `json:"event_id"`
	OccurredAt               time.Time                  `json:"occurred_at"`
	Label                    EpisodeLabel               `json:"label"`
	HistoricalSucceeded      bool                       `json:"historical_succeeded"`
	AssociationDecision      string                     `json:"association_decision,omitempty"`
	ChainCreated             bool                       `json:"chain_created"`
	HypothesisAction         string                     `json:"hypothesis_action,omitempty"`
	RoutinePresenceApplied   bool                       `json:"routine_presence_applied"`
	RoutineTransitionApplied bool                       `json:"routine_transition_applied"`
	DeviationAttempted       bool                       `json:"deviation_attempted"`
	DeviationStatus          deviation.EvaluationStatus `json:"deviation_status,omitempty"`
	DeviationBand            deviation.Band             `json:"deviation_band,omitempty"`
	DeviationScore           deviation.Score            `json:"deviation_score"`
	DeviationCoverage        deviation.Score            `json:"deviation_coverage"`
	JournalSequence          uint64                     `json:"journal_sequence"`
	Restarted                bool                       `json:"restarted"`
	Checkpointed             bool                       `json:"checkpointed"`
	ErrorCode                string                     `json:"error_code,omitempty"`
}

type WarmupMetrics struct {
	FirstEvaluatedAt            *time.Time `json:"first_evaluated_at,omitempty"`
	EventsBeforeFirstEvaluation int        `json:"events_before_first_evaluation"`
	DaysBeforeFirstEvaluation   int        `json:"days_before_first_evaluation"`
	InsufficientHistoryCount    int        `json:"insufficient_history_count"`
	FirstPresenceEvaluatedAt    *time.Time `json:"first_presence_evaluated_at,omitempty"`
	FirstTransitionEvaluatedAt  *time.Time `json:"first_transition_evaluated_at,omitempty"`
}

type LabelMetrics struct {
	Label                    EpisodeLabel    `json:"label"`
	EventCount               int             `json:"event_count"`
	EvaluatedCount           int             `json:"evaluated_count"`
	PartialCount             int             `json:"partial_count"`
	InsufficientHistoryCount int             `json:"insufficient_history_count"`
	AmbiguousCount           int             `json:"ambiguous_count"`
	AlreadyEvaluatedCount    int             `json:"already_evaluated_count"`
	NotApplicableCount       int             `json:"not_applicable_count"`
	AlignedCount             int             `json:"aligned_count"`
	LowCount                 int             `json:"low_count"`
	ModerateCount            int             `json:"moderate_count"`
	HighCount                int             `json:"high_count"`
	MeanScore                float64         `json:"mean_score"`
	MedianScore              deviation.Score `json:"median_score"`
	P90Score                 deviation.Score `json:"p90_score"`
	P95Score                 deviation.Score `json:"p95_score"`
	MaximumScore             deviation.Score `json:"maximum_score"`
	MeanCoverage             float64         `json:"mean_coverage"`
}

type BenignDeviationMetrics struct {
	EvaluatedEvents            int `json:"evaluated_events"`
	ModerateOrHighCount        int `json:"moderate_or_high_count"`
	HighCount                  int `json:"high_count"`
	ModerateOrHighRatePermille int `json:"moderate_or_high_rate_permille"`
	HighRatePermille           int `json:"high_rate_permille"`
}

type SeparationMetrics struct {
	OrdinaryMedian          deviation.Score `json:"ordinary_median"`
	BenignMedian            deviation.Score `json:"benign_median"`
	SyntheticMedian         deviation.Score `json:"synthetic_median"`
	OrdinaryP95             deviation.Score `json:"ordinary_p95"`
	SyntheticP50            deviation.Score `json:"synthetic_p50"`
	MedianGap               int             `json:"median_gap"`
	QuantileOverlapPermille int             `json:"quantile_overlap_permille"`
}

type AdaptationMetrics struct {
	ChangeStartedAt           time.Time  `json:"change_started_at"`
	FirstHighAt               *time.Time `json:"first_high_at,omitempty"`
	LastHighAt                *time.Time `json:"last_high_at,omitempty"`
	FirstAlignedAfterChange   *time.Time `json:"first_aligned_after_change,omitempty"`
	EventsUntilAligned        int        `json:"events_until_aligned"`
	DaysUntilAligned          int        `json:"days_until_aligned"`
	NewRoutineOccurrenceCount int        `json:"new_routine_occurrence_count"`
}

type GrowthMetrics struct {
	At                        time.Time `json:"at"`
	EventCount                int       `json:"event_count"`
	ChainCount                int       `json:"chain_count"`
	HypothesisCount           int       `json:"hypothesis_count"`
	RoutineCount              int       `json:"routine_count"`
	RoutineOccurrenceCount    int       `json:"routine_occurrence_count"`
	JournalRecords            uint64    `json:"journal_records"`
	JournalBytes              int64     `json:"journal_bytes"`
	GenerationCount           int       `json:"generation_count"`
	RecentDeviationStoreCount int       `json:"recent_deviation_store_count"`
}

type DurationSummary struct {
	Count   int           `json:"count"`
	Status  string        `json:"status"`
	Median  time.Duration `json:"median"`
	P90     time.Duration `json:"p90"`
	P95     time.Duration `json:"p95"`
	P99     time.Duration `json:"p99"`
	Maximum time.Duration `json:"maximum"`
}

type LatencyMetrics struct {
	Total       DurationSummary `json:"total"`
	Association DurationSummary `json:"association"`
	Deviation   DurationSummary `json:"deviation"`
	Learning    DurationSummary `json:"learning"`
	WAL         DurationSummary `json:"wal"`
}

type MemoryMetric struct {
	Kind   string `json:"kind"`
	Value  int64  `json:"value"`
	Unit   string `json:"unit"`
	Status string `json:"status"`
}

type CalibrationFindingSeverity string

const (
	SeverityInfo     CalibrationFindingSeverity = "info"
	SeverityWarning  CalibrationFindingSeverity = "warning"
	SeverityBlocking CalibrationFindingSeverity = "blocking_for_authority"
)

type CalibrationFinding struct {
	Code     string                     `json:"code"`
	Severity CalibrationFindingSeverity `json:"severity"`
	Message  string                     `json:"message"`
}

type InvariantFailure struct {
	Code    string    `json:"code"`
	At      time.Time `json:"at"`
	EventID string    `json:"event_id,omitempty"`
}

type Report struct {
	CampaignID          string                 `json:"campaign_id"`
	ProfileID           string                 `json:"profile_id"`
	Seed                uint64                 `json:"seed"`
	StartedAt           time.Time              `json:"started_at"`
	EndedAt             time.Time              `json:"ended_at"`
	SimulatedStart      time.Time              `json:"simulated_start"`
	SimulatedEnd        time.Time              `json:"simulated_end"`
	Configuration       ConfigurationSnapshot  `json:"configuration"`
	EventCount          int                    `json:"event_count"`
	EventsSucceeded     int                    `json:"events_succeeded"`
	EventsFailed        int                    `json:"events_failed"`
	Warmup              WarmupMetrics          `json:"warmup"`
	Labels              []LabelMetrics         `json:"labels"`
	BenignDeviation     BenignDeviationMetrics `json:"benign_deviation"`
	Separation          SeparationMetrics      `json:"separation"`
	Adaptation          *AdaptationMetrics     `json:"adaptation,omitempty"`
	Growth              []GrowthMetrics        `json:"growth"`
	Memory              []MemoryMetric         `json:"memory"`
	Latency             LatencyMetrics         `json:"latency"`
	RestartCount        int                    `json:"restart_count"`
	CheckpointCount     int                    `json:"checkpoint_count"`
	IdempotenceChecks   int                    `json:"idempotence_checks"`
	IdempotenceFailures int                    `json:"idempotence_failures"`
	DurableStateDigest  string                 `json:"durable_state_digest"`
	InvariantFailures   []InvariantFailure     `json:"invariant_failures,omitempty"`
	CalibrationFindings []CalibrationFinding   `json:"calibration_findings,omitempty"`
	Success             bool                   `json:"success"`
	BlockingReasons     []string               `json:"blocking_reasons,omitempty"`
	Events              []EventResult          `json:"events,omitempty"`
}

type RunOptions struct {
	RootDir      string
	Full         bool
	DaysOverride int
	EventsOutput string
}
