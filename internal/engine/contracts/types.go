package contracts

import (
	"sync"
	"time"
)

type HouseState string

const (
	HouseStateUnknown  HouseState = "unknown"
	HouseStateOccupied HouseState = "occupied"
	HouseStateEmpty    HouseState = "empty"
	HouseStateSleeping HouseState = "sleeping"
)

type SubjectType string

const (
	SubjectResident SubjectType = "resident"
	SubjectUnknown  SubjectType = "unknown"
	SubjectDevice   SubjectType = "device"
	SubjectSystem   SubjectType = "system"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type DivergenceReason string

const (
	DivergenceUnexpectedEvent    DivergenceReason = "unexpected_event"
	DivergenceUnexpectedTopology DivergenceReason = "unexpected_topology"
	DivergenceUnexpectedTime     DivergenceReason = "unexpected_time"
	DivergenceUnexpectedSequence DivergenceReason = "unexpected_sequence"
	DivergenceRarePath           DivergenceReason = "rare_path"
)

type OutcomeType string

const (
	OutcomeUnknown OutcomeType = "unknown"

	OutcomeSafe OutcomeType = "safe"

	OutcomeWarning OutcomeType = "warning"

	OutcomeDanger OutcomeType = "danger"

	OutcomeEmergency OutcomeType = "emergency"
)

type RuntimeState struct {
	StartedAt time.Time

	ActiveTracks map[string]*Track

	Residents map[string]*ResidentState

	Occupancy map[string][]string

	RecentEvents []*Event

	AlertHistory []*AlertRecord

	Novelty map[string]*NoveltyRecord

	HouseState HouseState

	Version uint64

	mu sync.RWMutex
}

type ResidentState struct {
	ResidentID string

	CurrentNode string

	LastSeen time.Time

	CurrentTrackID string

	Confidence float64

	Active bool
}

type Event struct {
	ID string

	Type string

	SubjectType SubjectType
	SubjectID   string

	TargetType SubjectType
	TargetID   string

	TopologyNode string

	Timestamp time.Time

	TrackID string

	Confidence float64

	Severity Severity

	Metadata map[string]any
}

type Track struct {
	ID string

	SubjectType SubjectType
	SubjectID   string

	TopologyPath []string

	StartedAt  time.Time
	CreatedAt  time.Time
	LastUpdate time.Time

	ClosedAt *time.Time

	CurrentNode string

	Events []*Event

	Confidence float64

	Active bool

	Context map[string]any
}

type BehaviorNode struct {
	Event string

	SubjectType SubjectType
	SubjectID   string

	TargetType SubjectType
	TargetID   string

	TopologyNode string

	Weight float64

	Count uint64

	AvgDeltaMs int64

	LastSeen time.Time

	Outcome *Outcome

	Context map[string]any

	Children []*BehaviorNode
}

type BehaviorGraph struct {
	GraphID string

	Roots []*BehaviorNode

	Version uint64

	LastUpdate time.Time
}

type LearnedSequence struct {
	ID                string    `json:"id"`
	Signature         string    `json:"signature"`
	Name              string    `json:"name,omitempty"`
	EventTypes        []string  `json:"event_types"`
	SourceTypes       []string  `json:"source_types"`
	Devices           []string  `json:"devices"`
	Nodes             []string  `json:"nodes"`
	Identities        []string  `json:"identities"`
	Count             int       `json:"count"`
	FirstSeen         time.Time `json:"first_seen"`
	LastSeen          time.Time `json:"last_seen"`
	AvgDeltaMs        int64     `json:"avg_delta_ms"`
	Confidence        float64   `json:"confidence"`
	Origin            string    `json:"origin,omitempty"`
	CriticalSeedID    string    `json:"critical_seed_id,omitempty"`
	DangerScore       float64   `json:"danger_score,omitempty"`
	RiskLevel         string    `json:"risk_level,omitempty"`
	ExpectedState     string    `json:"expected_state,omitempty"`
	SimulatedCount    int       `json:"simulated_count"`
	RealCount         int       `json:"real_count"`
	CriticalSeedCount int       `json:"critical_seed_count,omitempty"`
	SeedCount         int       `json:"seed_count,omitempty"`
	EffectiveCount    int       `json:"effective_count,omitempty"`
	ConfidenceBase    float64   `json:"confidence_base,omitempty"`
	LastTestRunID     string    `json:"last_test_run_id,omitempty"`
	LastScenarioID    string    `json:"last_scenario_id,omitempty"`
	Examples          []string  `json:"examples,omitempty"`
	Evidence          []string  `json:"evidence,omitempty"`
}

type LearnedTransition struct {
	ID             string    `json:"id"`
	FromEventType  string    `json:"from_event_type"`
	ToEventType    string    `json:"to_event_type"`
	FromSignature  string    `json:"from_signature"`
	ToSignature    string    `json:"to_signature"`
	Count          int       `json:"count"`
	AvgDeltaMs     int64     `json:"avg_delta_ms"`
	Confidence     float64   `json:"confidence"`
	FirstSeen      time.Time `json:"first_seen"`
	LastSeen       time.Time `json:"last_seen"`
	SimulatedCount int       `json:"simulated_count"`
	RealCount      int       `json:"real_count"`
}

type LearnedBehavior struct {
	ID                       string           `json:"id"`
	TriggerSequenceSignature string           `json:"trigger_sequence_signature"`
	Context                  map[string]any   `json:"context,omitempty"`
	ProposedActions          []map[string]any `json:"proposed_actions"`
	Count                    int              `json:"count"`
	Confidence               float64          `json:"confidence"`
	Origin                   string           `json:"origin,omitempty"`
	CriticalSeedID           string           `json:"critical_seed_id,omitempty"`
	DangerScore              float64          `json:"danger_score,omitempty"`
	RiskLevel                string           `json:"risk_level,omitempty"`
	ExpectedState            string           `json:"expected_state,omitempty"`
	Status                   string           `json:"status"`
	Evidence                 []string         `json:"evidence,omitempty"`
	SimulatedCount           int              `json:"simulated_count"`
	RealCount                int              `json:"real_count"`
	CriticalSeedCount        int              `json:"critical_seed_count,omitempty"`
	SeedCount                int              `json:"seed_count,omitempty"`
	EffectiveCount           int              `json:"effective_count,omitempty"`
	ConfidenceBase           float64          `json:"confidence_base,omitempty"`
	LastMatchedAt            time.Time        `json:"last_matched_at,omitempty"`
	LastTriggeredAt          *time.Time       `json:"last_triggered_at,omitempty"`
	RequiresValidation       bool             `json:"requires_validation"`
	ForbiddenActions         []string         `json:"forbidden_actions,omitempty"`
}

type CriticalSeedStep struct {
	EventType string `json:"event_type" yaml:"event_type"`
	ZoneRole  string `json:"zone_role,omitempty" yaml:"zone_role,omitempty"`
}

type CriticalSeed struct {
	ID                 string             `json:"id" yaml:"id"`
	Name               string             `json:"name" yaml:"name"`
	Description        string             `json:"description,omitempty" yaml:"description,omitempty"`
	DangerScore        float64            `json:"danger_score" yaml:"danger_score"`
	RiskLevel          string             `json:"risk_level" yaml:"risk_level"`
	ExpectedState      string             `json:"expected_state" yaml:"expected_state"`
	Sequence           []CriticalSeedStep `json:"sequence" yaml:"sequence"`
	Context            map[string]any     `json:"context,omitempty" yaml:"context,omitempty"`
	ProposedActions    []string           `json:"proposed_actions,omitempty" yaml:"proposed_actions,omitempty"`
	ForbiddenActions   []string           `json:"forbidden_actions,omitempty" yaml:"forbidden_actions,omitempty"`
	RequiresValidation bool               `json:"requires_validation" yaml:"requires_validation"`
	Enabled            bool               `json:"enabled" yaml:"enabled"`
}

type CriticalSeedMatch struct {
	CriticalSeedID      string   `json:"critical_seed_id"`
	Name                string   `json:"name,omitempty"`
	Signature           string   `json:"signature"`
	ExpectedState       string   `json:"expected_state"`
	ActualState         string   `json:"actual_state,omitempty"`
	ExpectedDangerScore float64  `json:"expected_danger_score"`
	RiskLevel           string   `json:"risk_level"`
	Passed              bool     `json:"passed"`
	Failures            []string `json:"failures,omitempty"`
}

type DivergenceResult struct {
	Score float64

	Level Severity

	Reasons []DivergenceReason

	ExpectedNodes []*BehaviorNode

	ReceivedEvent *Event

	Novel bool

	Known bool

	Timestamp time.Time
}

type AlertRecord struct {
	ID string

	EventType string

	TopologyNode string

	SubjectID string

	Score float64

	Level Severity

	Reasons []DivergenceReason

	Timestamp time.Time

	Resolved bool

	FalsePositive bool
}

type NoveltyRecord struct {
	ID string

	SubjectID string

	EventType string

	TopologyNode string

	Count uint64

	FirstSeen time.Time

	LastSeen time.Time

	LastScore float64

	Resolved bool

	Promoted bool
}

type Outcome struct {
	Type OutcomeType

	Value float64

	Confidence float64

	SuccessCount uint64

	FailureCount uint64

	LastValidated time.Time
}

type Situation struct {
	ID string

	Type string

	Severity Severity

	NodeID string

	ResidentID string

	DeviceID string

	ClipID string

	Evidence []string

	CreatedAt time.Time

	ExpiresAt *time.Time
}

type DecisionResult struct {
	DivergenceScore float64
	DecisionScore   float64

	GraphTrust     float64
	GuidelineTrust float64

	OutcomeValue   float64
	GuidelineValue float64

	Level Severity

	Reasons  []string
	Evidence []string

	SequenceKey string
	GraphUsed   bool

	ValidationRequired bool
	ValidationReason   string

	Outcome *Outcome

	Situations []Situation

	Timestamp time.Time
}

type SimilarityResult struct {
	EventSimilarity       float64
	TopologySimilarity    float64
	TimeSimilarity        float64
	StatisticalSimilarity float64

	Similarity float64
	Divergence float64
}

type ActiveSequence struct {
	ID string

	SubjectID string

	StartedAt time.Time

	LastUpdate time.Time

	Events []*Event

	CurrentNode *BehaviorNode

	Predictions []*BehaviorNode

	Outcome *Outcome

	Closed bool
}
