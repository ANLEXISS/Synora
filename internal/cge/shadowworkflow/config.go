package shadowworkflow

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"synora/internal/cge/calibrationanalytics"
	"synora/internal/cge/calibrationledger"
)

type PipelineDepth string

const (
	DepthEpisode                PipelineDepth = "episode"
	DepthSituationFacts         PipelineDepth = "situation_facts"
	DepthSituationHypotheses    PipelineDepth = "situation_hypotheses"
	DepthEvidenceDiscrimination PipelineDepth = "evidence_discrimination"
	DepthAdvisoryRequests       PipelineDepth = "advisory_requests"
	DepthCapabilityMapping      PipelineDepth = "capability_mapping"
	DepthAuthorizationBoundary  PipelineDepth = "authorization_boundary"
)

type StoreMode string

const (
	StoreMemory StoreMode = "memory"
	StoreFile   StoreMode = "file"
)

type Config struct {
	Enabled                     bool
	PipelineDepth               PipelineDepth
	QueueCapacity               int
	WorkerCount                 int
	MaxProcessingDuration       time.Duration
	MaxInputAge                 time.Duration
	MaxEpisodes                 int
	MaxAdvisoryRequests         int
	MaxMappingsPerCycle         int
	MaxAuthorizationsPerCycle   int
	CheckpointEveryTransactions uint64
	CheckpointInterval          time.Duration
	MaxWALBytes                 int64
	MaxCheckpointBytes          int64
	ConsecutiveFailureLimit     int
	CircuitResetAfter           time.Duration
	StoreMode                   StoreMode
	StoreDirectory              string
	SyncOnCommit                bool
	AllowTruncatedFinalRecord   bool
	Qualification               QualificationConfig
	CalibrationLedger           CalibrationLedgerConfig
	CalibrationAnalytics        CalibrationAnalyticsConfig
}

type CalibrationLedgerConfig struct {
	Enabled              bool
	Path                 string
	Fsync                bool
	MaxBytes             int64
	MaxRecords           uint64
	RepairTrailingRecord bool
	Policy               calibrationledger.Policy
	Store                calibrationledger.Store
}

type CalibrationAnalyticsConfig struct {
	Enabled               bool
	Policy                calibrationanalytics.AnalyticsPolicy
	RecomputeEveryRecords uint64
}

const (
	CalibrationLedgerEnabledEnv          = "SYNORA_CGE_CALIBRATION_LEDGER_ENABLED"
	CalibrationLedgerPathEnv             = "SYNORA_CGE_CALIBRATION_LEDGER_PATH"
	CalibrationLedgerFsyncEnv            = "SYNORA_CGE_CALIBRATION_LEDGER_FSYNC"
	CalibrationLedgerMaxBytesEnv         = "SYNORA_CGE_CALIBRATION_LEDGER_MAX_BYTES"
	CalibrationLedgerMaxRecordsEnv       = "SYNORA_CGE_CALIBRATION_LEDGER_MAX_RECORDS"
	CalibrationLedgerRepairTrailingEnv   = "SYNORA_CGE_CALIBRATION_LEDGER_REPAIR_TRAILING_RECORD"
	CalibrationAnalyticsEnabledEnv       = "SYNORA_CGE_CALIBRATION_ANALYTICS_ENABLED"
	CalibrationAnalyticsMinRecordsEnv    = "SYNORA_CGE_CALIBRATION_ANALYTICS_MIN_RECORDS"
	CalibrationAnalyticsMinComparableEnv = "SYNORA_CGE_CALIBRATION_ANALYTICS_MIN_COMPARABLE"
	CalibrationAnalyticsWindowSizeEnv    = "SYNORA_CGE_CALIBRATION_ANALYTICS_WINDOW_SIZE"
	CalibrationAnalyticsMaxWindowsEnv    = "SYNORA_CGE_CALIBRATION_ANALYTICS_MAX_WINDOWS"
	DefaultCalibrationLedgerPath         = "/var/lib/synora/cge/calibration-ledger.ndjson"
)

func DefaultCalibrationLedgerConfig() CalibrationLedgerConfig {
	p := calibrationledger.DefaultPolicy()
	return CalibrationLedgerConfig{Fsync: true, MaxBytes: p.MaxLedgerBytes, MaxRecords: p.MaxRecords, Policy: p}
}

func DefaultCalibrationAnalyticsConfig() CalibrationAnalyticsConfig {
	return CalibrationAnalyticsConfig{Policy: calibrationanalytics.DefaultAnalyticsPolicy(), RecomputeEveryRecords: 100}
}

func (c CalibrationAnalyticsConfig) Validate() error {
	if err := c.Policy.Validate(); err != nil || c.RecomputeEveryRecords == 0 {
		return ErrInvalidConfig
	}
	return nil
}

func (c CalibrationLedgerConfig) effectivePolicy() calibrationledger.Policy {
	p := c.Policy
	p.MaxLedgerBytes = c.MaxBytes
	p.MaxRecords = c.MaxRecords
	p.Fsync = c.Fsync
	p.RepairTrailingRecord = c.RepairTrailingRecord
	return p
}

func (c CalibrationLedgerConfig) Validate() error {
	if err := c.Policy.Validate(); err != nil || c.MaxBytes <= 0 || c.MaxRecords == 0 {
		return ErrInvalidConfig
	}
	if c.Enabled && c.Store == nil && (strings.TrimSpace(c.Path) == "" || !filepath.IsAbs(c.Path) || filepath.Clean(c.Path) != c.Path) {
		return ErrInvalidConfig
	}
	return nil
}

func LoadCalibrationLedgerConfig(getenv func(string) string) (CalibrationLedgerConfig, error) {
	if getenv == nil {
		return CalibrationLedgerConfig{}, ErrInvalidConfig
	}
	c := DefaultCalibrationLedgerConfig()
	var err error
	if c.Enabled, err = parseQualificationBool(getenv(CalibrationLedgerEnabledEnv), false); err != nil {
		return CalibrationLedgerConfig{}, ErrInvalidConfig
	}
	c.Path = strings.TrimSpace(getenv(CalibrationLedgerPathEnv))
	if c.Path == "" {
		c.Path = DefaultCalibrationLedgerPath
	}
	if c.Fsync, err = parseQualificationBool(getenv(CalibrationLedgerFsyncEnv), true); err != nil {
		return CalibrationLedgerConfig{}, ErrInvalidConfig
	}
	if c.RepairTrailingRecord, err = parseQualificationBool(getenv(CalibrationLedgerRepairTrailingEnv), false); err != nil {
		return CalibrationLedgerConfig{}, ErrInvalidConfig
	}
	if v := strings.TrimSpace(getenv(CalibrationLedgerMaxBytesEnv)); v != "" {
		c.MaxBytes, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return CalibrationLedgerConfig{}, ErrInvalidConfig
		}
	}
	if v := strings.TrimSpace(getenv(CalibrationLedgerMaxRecordsEnv)); v != "" {
		c.MaxRecords, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			return CalibrationLedgerConfig{}, ErrInvalidConfig
		}
	}
	c.Policy.MaxLedgerBytes, c.Policy.MaxRecords, c.Policy.Fsync, c.Policy.RepairTrailingRecord = c.MaxBytes, c.MaxRecords, c.Fsync, c.RepairTrailingRecord
	if err := c.Validate(); err != nil {
		return CalibrationLedgerConfig{}, err
	}
	return c, nil
}

func LoadCalibrationAnalyticsConfig(getenv func(string) string) (CalibrationAnalyticsConfig, error) {
	if getenv == nil {
		return CalibrationAnalyticsConfig{}, ErrInvalidConfig
	}
	c := DefaultCalibrationAnalyticsConfig()
	var err error
	if c.Enabled, err = parseQualificationBool(getenv(CalibrationAnalyticsEnabledEnv), false); err != nil {
		return CalibrationAnalyticsConfig{}, ErrInvalidConfig
	}
	if v := strings.TrimSpace(getenv(CalibrationAnalyticsMinRecordsEnv)); v != "" {
		c.Policy.MinimumRecords, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			return CalibrationAnalyticsConfig{}, ErrInvalidConfig
		}
	}
	if v := strings.TrimSpace(getenv(CalibrationAnalyticsMinComparableEnv)); v != "" {
		c.Policy.MinimumComparableRecords, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			return CalibrationAnalyticsConfig{}, ErrInvalidConfig
		}
	}
	if v := strings.TrimSpace(getenv(CalibrationAnalyticsWindowSizeEnv)); v != "" {
		c.Policy.WindowSizeRecords, err = strconv.ParseUint(v, 10, 64)
		if err != nil {
			return CalibrationAnalyticsConfig{}, ErrInvalidConfig
		}
	}
	if v := strings.TrimSpace(getenv(CalibrationAnalyticsMaxWindowsEnv)); v != "" {
		c.Policy.MaximumWindows, err = strconv.Atoi(v)
		if err != nil {
			return CalibrationAnalyticsConfig{}, ErrInvalidConfig
		}
	}
	if err := c.Validate(); err != nil {
		return CalibrationAnalyticsConfig{}, err
	}
	return c, nil
}

func DefaultConfig() Config {
	return Config{
		PipelineDepth: DepthAdvisoryRequests, QueueCapacity: 128, WorkerCount: 1,
		MaxProcessingDuration: 250 * time.Millisecond, MaxInputAge: 10 * time.Minute,
		MaxEpisodes: 1000, MaxAdvisoryRequests: 16, MaxMappingsPerCycle: 8, MaxAuthorizationsPerCycle: 8,
		CheckpointEveryTransactions: 100, CheckpointInterval: 15 * time.Minute,
		MaxWALBytes: 256 * 1024 * 1024, MaxCheckpointBytes: 256 * 1024 * 1024,
		ConsecutiveFailureLimit: 5, CircuitResetAfter: 5 * time.Minute,
		StoreMode: StoreMemory, SyncOnCommit: true, AllowTruncatedFinalRecord: true, Qualification: DefaultQualificationConfig(), CalibrationLedger: DefaultCalibrationLedgerConfig(), CalibrationAnalytics: DefaultCalibrationAnalyticsConfig(),
	}
}

func (c Config) Validate() error {
	if err := c.Qualification.Validate(); err != nil {
		return err
	}
	if err := c.CalibrationLedger.Validate(); err != nil {
		return err
	}
	if err := c.CalibrationAnalytics.Validate(); err != nil {
		return err
	}
	if c.CalibrationAnalytics.Enabled && !c.CalibrationLedger.Enabled {
		return ErrInvalidConfig
	}
	if !c.Enabled {
		if c.Qualification.Enabled || c.CalibrationLedger.Enabled || c.CalibrationAnalytics.Enabled {
			return ErrInvalidConfig
		}
		return nil
	}
	if c.QueueCapacity <= 0 || c.WorkerCount != 1 || c.MaxProcessingDuration <= 0 || c.MaxInputAge <= 0 || c.MaxEpisodes <= 0 || c.MaxAdvisoryRequests <= 0 || c.MaxMappingsPerCycle <= 0 || c.MaxAuthorizationsPerCycle <= 0 || c.CheckpointEveryTransactions == 0 || c.CheckpointInterval <= 0 || c.MaxWALBytes <= 0 || c.MaxCheckpointBytes <= 0 || c.ConsecutiveFailureLimit <= 0 || c.CircuitResetAfter <= 0 {
		return ErrInvalidConfig
	}
	switch c.PipelineDepth {
	case DepthEpisode, DepthSituationFacts, DepthSituationHypotheses, DepthEvidenceDiscrimination, DepthAdvisoryRequests, DepthCapabilityMapping, DepthAuthorizationBoundary:
	default:
		return ErrInvalidConfig
	}
	if c.StoreMode != StoreMemory && c.StoreMode != StoreFile {
		return ErrInvalidConfig
	}
	if c.StoreMode == StoreFile && (strings.TrimSpace(c.StoreDirectory) == "" || !filepath.IsAbs(c.StoreDirectory)) {
		return ErrInvalidConfig
	}
	return nil
}

func (c Config) String() string {
	return fmt.Sprintf("enabled=%t depth=%s store=%s", c.Enabled, c.PipelineDepth, c.StoreMode)
}
