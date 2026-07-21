package shadowworkflow

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
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
}

func DefaultConfig() Config {
	return Config{
		PipelineDepth: DepthAdvisoryRequests, QueueCapacity: 128, WorkerCount: 1,
		MaxProcessingDuration: 250 * time.Millisecond, MaxInputAge: 10 * time.Minute,
		MaxEpisodes: 1000, MaxAdvisoryRequests: 16, MaxMappingsPerCycle: 8, MaxAuthorizationsPerCycle: 8,
		CheckpointEveryTransactions: 100, CheckpointInterval: 15 * time.Minute,
		MaxWALBytes: 256 * 1024 * 1024, MaxCheckpointBytes: 256 * 1024 * 1024,
		ConsecutiveFailureLimit: 5, CircuitResetAfter: 5 * time.Minute,
		StoreMode: StoreMemory, SyncOnCommit: true, AllowTruncatedFinalRecord: true, Qualification: DefaultQualificationConfig(),
	}
}

func (c Config) Validate() error {
	if err := c.Qualification.Validate(); err != nil {
		return err
	}
	if !c.Enabled {
		if c.Qualification.Enabled {
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
