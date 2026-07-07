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
