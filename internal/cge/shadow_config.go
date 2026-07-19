package cge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"synora/internal/cge/chains/association"
	"synora/internal/cge/chains/evidence"
	"synora/internal/cge/deviation"
	"synora/internal/cge/fieldtrial"
	"synora/internal/cge/routines"
)

const (
	DefaultShadowDataDir                        = "/var/lib/synora/cge"
	DefaultShadowJournalName                    = "journal.ndjson"
	DefaultShadowActor                          = "synora-core/cge-shadow"
	ShadowEnabledEnv                            = "SYNORA_CGE_SHADOW_ENABLED"
	ShadowDataDirEnv                            = "SYNORA_CGE_DATA_DIR"
	ShadowJournalPathEnv                        = "SYNORA_CGE_JOURNAL_PATH"
	ShadowInitializeEnv                         = "SYNORA_CGE_INITIALIZE_IF_MISSING"
	ShadowJournalIDEnv                          = "SYNORA_CGE_JOURNAL_ID"
	ShadowCognitiveEnabledEnv                   = "SYNORA_CGE_SHADOW_COGNITIVE_ENABLED"
	ShadowAutoEvidenceEnv                       = "SYNORA_CGE_SHADOW_AUTO_EVIDENCE_ENABLED"
	ShadowMaxReevaluationsEnv                   = "SYNORA_CGE_SHADOW_MAX_EVIDENCE_REEVALUATIONS"
	ShadowContextEnabledEnv                     = "SYNORA_CGE_SHADOW_CONTEXT_ENABLED"
	ShadowContextTimezoneEnv                    = "SYNORA_CGE_SHADOW_CONTEXT_TIMEZONE"
	ShadowContextAllowPartialEnv                = "SYNORA_CGE_SHADOW_CONTEXT_ALLOW_PARTIAL"
	ShadowRoutineLearningEnabledEnv             = "SYNORA_CGE_SHADOW_ROUTINE_LEARNING_ENABLED"
	ShadowRoutineBucketEnv                      = "SYNORA_CGE_SHADOW_ROUTINE_TEMPORAL_BUCKET_MINUTES"
	ShadowRoutineAllowPartialEnv                = "SYNORA_CGE_SHADOW_ROUTINE_ALLOW_PARTIAL"
	ShadowRoutineMaxGapEnv                      = "SYNORA_CGE_SHADOW_ROUTINE_MAX_TRANSITION_GAP"
	ShadowRoutineSameTopologyEnv                = "SYNORA_CGE_SHADOW_ROUTINE_REQUIRE_SAME_TOPOLOGY_REVISION"
	ShadowDeviationEnabledEnv                   = "SYNORA_CGE_SHADOW_DEVIATION_ENABLED"
	ShadowDeviationRecentLimitEnv               = "SYNORA_CGE_SHADOW_DEVIATION_RECENT_LIMIT"
	ShadowDeviationMaxAssessmentsEnv            = "SYNORA_CGE_SHADOW_DEVIATION_MAX_ASSESSMENTS_PER_OBSERVATION"
	MaxShadowEvidenceReevaluations              = 64
	MaxShadowDeviationRecentAssessments         = 4096
	MaxShadowDeviationAssessmentsPerObservation = 4
)

var (
	ErrInvalidShadowConfig = errors.New("invalid_cge_shadow_config")
	ErrShadowStartup       = errors.New("cge_shadow_startup_failed")
	ErrShadowDisabled      = errors.New("cge_shadow_disabled")
)

// ShadowConfig controls the optional durable shadow integration.
type ShadowConfig struct {
	Enabled bool

	DataDir     string
	JournalPath string

	InitializeIfMissing bool
	JournalID           string
	// JournalOnlyRecovery is a development/recovery override used to verify
	// journal-only reconstruction when a generation manifest also exists.
	JournalOnlyRecovery bool

	Actor string

	AssociationPolicy association.Policy
	EvidencePolicy    evidence.Policy
	Cognitive         CognitiveShadowConfig
	Context           ShadowContextConfig
	Routines          ShadowRoutineConfig
	Deviation         ShadowDeviationConfig
	FieldTrial        fieldtrial.Config

	EligibleEventTypes []string
}

// ShadowContextConfig controls descriptive context capture. It is disabled
// independently from cognitive orchestration and never changes a policy.
type ShadowContextConfig struct {
	Enabled      bool
	Timezone     string
	AllowPartial bool
}

// CognitiveShadowConfig controls the optional observation-to-evidence
// orchestration layer. It is separate from historical Shadow operation.
type CognitiveShadowConfig struct {
	Enabled                                bool
	AutoApplyDecisiveEvidence              bool
	MaxEvidenceReevaluationsPerObservation int
}

type ShadowRoutineConfig struct {
	Enabled                     bool
	TemporalBucketMinutes       int
	AllowPartialContext         bool
	MaxTransitionGap            time.Duration
	RequireSameTopologyRevision bool
}

type ShadowDeviationConfig struct {
	Enabled                      bool
	RecentAssessmentLimit        int
	MaxAssessmentsPerObservation int
	Policy                       deviation.Policy
}

// DefaultShadowConfig is disabled and uses a conservative identity allowlist.
func DefaultShadowConfig() ShadowConfig {
	dataDir := DefaultShadowDataDir
	return ShadowConfig{
		DataDir: dataDir, JournalPath: filepath.Join(dataDir, DefaultShadowJournalName),
		Actor: DefaultShadowActor, AssociationPolicy: association.DefaultPolicy(), EvidencePolicy: evidence.DefaultPolicy(),
		Cognitive:          CognitiveShadowConfig{MaxEvidenceReevaluationsPerObservation: 8},
		Context:            ShadowContextConfig{Timezone: "UTC", AllowPartial: true},
		Routines:           ShadowRoutineConfig{TemporalBucketMinutes: 15, AllowPartialContext: true, MaxTransitionGap: 15 * time.Minute, RequireSameTopologyRevision: true},
		Deviation:          ShadowDeviationConfig{RecentAssessmentLimit: 256, MaxAssessmentsPerObservation: 2, Policy: deviation.DefaultPolicy()},
		FieldTrial:         fieldtrial.DefaultConfig(),
		EligibleEventTypes: DefaultEligibleEventTypes(),
	}
}

// DefaultEligibleEventTypes returns a fresh conservative allowlist.
func DefaultEligibleEventTypes() []string {
	return []string{"vision.identity", "vision.unknown", "vision.uncertain"}
}

// LoadShadowConfig parses only the explicit environment settings for this
// pass. An invalid setting is an error; it is never silently corrected.
func LoadShadowConfig(getenv func(string) string) (ShadowConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	config := DefaultShadowConfig()
	var err error
	if config.Enabled, err = parseOptionalBool(getenv(ShadowEnabledEnv), false); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: enabled", ErrInvalidShadowConfig)
	}
	if value := getenv(ShadowDataDirEnv); value != "" {
		config.DataDir = value
	}
	if value := getenv(ShadowJournalPathEnv); value != "" {
		config.JournalPath = value
	} else {
		config.JournalPath = filepath.Join(config.DataDir, DefaultShadowJournalName)
	}
	if value := getenv(ShadowInitializeEnv); value != "" {
		if config.InitializeIfMissing, err = parseOptionalBool(value, false); err != nil {
			return ShadowConfig{}, fmt.Errorf("%w: initialize_if_missing", ErrInvalidShadowConfig)
		}
	}
	if value := getenv(ShadowJournalIDEnv); value != "" {
		config.JournalID = value
	}
	if config.Cognitive.Enabled, err = parseOptionalBool(getenv(ShadowCognitiveEnabledEnv), false); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: cognitive enabled", ErrInvalidShadowConfig)
	}
	if config.Cognitive.AutoApplyDecisiveEvidence, err = parseOptionalBool(getenv(ShadowAutoEvidenceEnv), false); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: auto evidence enabled", ErrInvalidShadowConfig)
	}
	if value := getenv(ShadowMaxReevaluationsEnv); value != "" {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil {
			return ShadowConfig{}, fmt.Errorf("%w: max evidence reevaluations", ErrInvalidShadowConfig)
		}
		config.Cognitive.MaxEvidenceReevaluationsPerObservation = parsed
	}
	if config.Context.Enabled, err = parseOptionalBool(getenv(ShadowContextEnabledEnv), false); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: context enabled", ErrInvalidShadowConfig)
	}
	if value := getenv(ShadowContextTimezoneEnv); value != "" {
		config.Context.Timezone = strings.TrimSpace(value)
	}
	if config.Context.AllowPartial, err = parseOptionalBool(getenv(ShadowContextAllowPartialEnv), true); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: context allow partial", ErrInvalidShadowConfig)
	}
	if config.Routines.Enabled, err = parseOptionalBool(getenv(ShadowRoutineLearningEnabledEnv), false); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: routine learning enabled", ErrInvalidShadowConfig)
	}
	if value := getenv(ShadowRoutineBucketEnv); value != "" {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil {
			return ShadowConfig{}, fmt.Errorf("%w: routine temporal bucket", ErrInvalidShadowConfig)
		}
		config.Routines.TemporalBucketMinutes = parsed
	}
	if config.Routines.AllowPartialContext, err = parseOptionalBool(getenv(ShadowRoutineAllowPartialEnv), true); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: routine allow partial", ErrInvalidShadowConfig)
	}
	if value := getenv(ShadowRoutineMaxGapEnv); value != "" {
		parsed, parseErr := time.ParseDuration(strings.TrimSpace(value))
		if parseErr != nil {
			return ShadowConfig{}, fmt.Errorf("%w: routine max transition gap", ErrInvalidShadowConfig)
		}
		config.Routines.MaxTransitionGap = parsed
	}
	if config.Routines.RequireSameTopologyRevision, err = parseOptionalBool(getenv(ShadowRoutineSameTopologyEnv), true); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: routine topology revision", ErrInvalidShadowConfig)
	}
	if config.Deviation.Enabled, err = parseOptionalBool(getenv(ShadowDeviationEnabledEnv), false); err != nil {
		return ShadowConfig{}, fmt.Errorf("%w: deviation enabled", ErrInvalidShadowConfig)
	}
	if value := getenv(ShadowDeviationRecentLimitEnv); value != "" {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil {
			return ShadowConfig{}, fmt.Errorf("%w: deviation recent limit", ErrInvalidShadowConfig)
		}
		config.Deviation.RecentAssessmentLimit = parsed
	}
	if value := getenv(ShadowDeviationMaxAssessmentsEnv); value != "" {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
		if parseErr != nil {
			return ShadowConfig{}, fmt.Errorf("%w: deviation max assessments", ErrInvalidShadowConfig)
		}
		config.Deviation.MaxAssessmentsPerObservation = parsed
	}
	fieldTrialConfig, fieldTrialErr := fieldtrial.LoadConfig(getenv)
	if fieldTrialErr != nil {
		return ShadowConfig{}, fmt.Errorf("%w: field trial: %v", ErrInvalidShadowConfig, fieldTrialErr)
	}
	config.FieldTrial = fieldTrialConfig
	if err := config.Validate(); err != nil {
		return ShadowConfig{}, err
	}
	return config, nil
}

func parseOptionalBool(value string, fallback bool) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, err
	}
	return parsed, nil
}

// Validate checks activation-specific filesystem, provenance, policy, and
// allowlist requirements. Disabled configuration performs no filesystem work.
func (c ShadowConfig) Validate() error {
	if !c.Enabled {
		// Cognitive settings have no effect while the whole ShadowEngine is
		// disabled; retain the historical zero-value compatibility boundary.
		return nil
	}
	if c.Cognitive.MaxEvidenceReevaluationsPerObservation <= 0 || c.Cognitive.MaxEvidenceReevaluationsPerObservation > MaxShadowEvidenceReevaluations {
		return fmt.Errorf("%w: max evidence reevaluations must be between 1 and %d", ErrInvalidShadowConfig, MaxShadowEvidenceReevaluations)
	}
	if c.Context.Enabled {
		if strings.TrimSpace(c.Context.Timezone) == "" {
			return fmt.Errorf("%w: context timezone is empty", ErrInvalidShadowConfig)
		}
		if _, err := time.LoadLocation(c.Context.Timezone); err != nil {
			return fmt.Errorf("%w: context timezone: %v", ErrInvalidShadowConfig, err)
		}
	}
	if c.Routines.Enabled {
		policy := routines.ExtractionPolicy{Namespace: "synora.cge.routines", Version: "routine-extraction-v1", TemporalBucketMinutes: c.Routines.TemporalBucketMinutes, AllowPartialContext: c.Routines.AllowPartialContext, MaxTransitionGap: c.Routines.MaxTransitionGap, RequireSameTopologyRevision: c.Routines.RequireSameTopologyRevision}
		if err := policy.Validate(); err != nil {
			return fmt.Errorf("%w: routine policy: %v", ErrInvalidShadowConfig, err)
		}
	}
	if c.Deviation.RecentAssessmentLimit < 0 || c.Deviation.RecentAssessmentLimit > MaxShadowDeviationRecentAssessments {
		return fmt.Errorf("%w: deviation recent limit must be between 0 and %d", ErrInvalidShadowConfig, MaxShadowDeviationRecentAssessments)
	}
	if c.Deviation.MaxAssessmentsPerObservation <= 0 || c.Deviation.MaxAssessmentsPerObservation > MaxShadowDeviationAssessmentsPerObservation {
		return fmt.Errorf("%w: deviation max assessments must be between 1 and %d", ErrInvalidShadowConfig, MaxShadowDeviationAssessmentsPerObservation)
	}
	if c.Deviation.Enabled {
		if err := c.Deviation.Policy.Validate(); err != nil {
			return fmt.Errorf("%w: deviation policy: %v", ErrInvalidShadowConfig, err)
		}
	}
	if err := c.FieldTrial.Validate(); err != nil {
		return fmt.Errorf("%w: field trial: %v", ErrInvalidShadowConfig, err)
	}
	if err := c.EvidencePolicy.Validate(); err != nil {
		return fmt.Errorf("%w: evidence policy: %v", ErrInvalidShadowConfig, err)
	}
	if err := validateAbsolutePath(c.DataDir, "data directory"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidShadowConfig, err)
	}
	if err := validateAbsolutePath(c.JournalPath, "journal path"); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidShadowConfig, err)
	}
	if strings.TrimSpace(c.JournalID) == "" && c.InitializeIfMissing {
		return fmt.Errorf("%w: journal id is required for initialization", ErrInvalidShadowConfig)
	}
	if c.InitializeIfMissing {
		if err := validateSafeText(c.JournalID, "journal id", 256, true); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidShadowConfig, err)
		}
	}
	if err := validateSafeText(c.Actor, "actor", 128, true); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidShadowConfig, err)
	}
	if err := c.AssociationPolicy.Validate(); err != nil {
		return fmt.Errorf("%w: association policy: %v", ErrInvalidShadowConfig, err)
	}
	if len(c.EligibleEventTypes) == 0 {
		return fmt.Errorf("%w: eligible event types are empty", ErrInvalidShadowConfig)
	}
	seen := make(map[string]struct{}, len(c.EligibleEventTypes))
	for _, eventType := range c.EligibleEventTypes {
		if err := validateSafeText(eventType, "eligible event type", 128, true); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidShadowConfig, err)
		}
		if strings.TrimSpace(eventType) != eventType || strings.ToLower(eventType) != eventType {
			return fmt.Errorf("%w: eligible event type must be normalized", ErrInvalidShadowConfig)
		}
		if _, exists := seen[eventType]; exists {
			return fmt.Errorf("%w: duplicate eligible event type", ErrInvalidShadowConfig)
		}
		seen[eventType] = struct{}{}
	}
	return nil
}

func validateAbsolutePath(value, field string) error {
	if strings.TrimSpace(value) == "" || filepath.IsAbs(value) == false || filepath.Clean(value) != value || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}

func validateSafeText(value, field string, max int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is empty", field)
	}
	if len([]rune(value)) > max || strings.ContainsAny(value, "\r\n") || strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s is invalid", field)
	}
	return nil
}
