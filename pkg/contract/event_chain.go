package contract

import "time"

type EventChainStatus string

const (
	EventChainOpen   EventChainStatus = "open"
	EventChainClosed EventChainStatus = "closed"
)

type DangerLevel string

const (
	DangerNone     DangerLevel = "none"
	DangerLow      DangerLevel = "low"
	DangerMedium   DangerLevel = "medium"
	DangerHigh     DangerLevel = "high"
	DangerCritical DangerLevel = "critical"
)

// PublicEvent is the redacted event representation exposed as part of a
// chain. Raw events remain available through the existing event store.
type PublicEvent struct {
	ID           string         `json:"id,omitempty"`
	Type         string         `json:"type"`
	Timestamp    time.Time      `json:"timestamp"`
	DeviceID     string         `json:"device_id,omitempty"`
	NodeID       string         `json:"node_id,omitempty"`
	ActivationID string         `json:"activation_id,omitempty"`
	SequenceKey  string         `json:"sequence_key,omitempty"`
	ClipID       string         `json:"clip_id,omitempty"`
	ClipIndex    int            `json:"clip_index,omitempty"`
	TrackID      string         `json:"track_id,omitempty"`
	Severity     string         `json:"severity,omitempty"`
	Significant  bool           `json:"significant"`
	Contextual   bool           `json:"contextual"`
	Simulated    bool           `json:"simulated,omitempty"`
	TestRunID    string         `json:"test_run_id,omitempty"`
	Payload      map[string]any `json:"payload,omitempty"`
}

type ChainEvaluation struct {
	Index              int       `json:"index"`
	EventID            string    `json:"event_id"`
	Timestamp          time.Time `json:"timestamp"`
	State              string    `json:"state,omitempty"`
	DangerLevel        string    `json:"danger_level"`
	DangerScore        float64   `json:"danger_score"`
	Reasons            []string  `json:"reasons,omitempty"`
	Hypotheses         []string  `json:"hypotheses,omitempty"`
	RecommendedActions []string  `json:"recommended_actions,omitempty"`
	ActionDecision     string    `json:"action_decision,omitempty"`
	BlockedActions     []string  `json:"blocked_actions,omitempty"`
	EngineVersion      string    `json:"engine_version,omitempty"`
}

type ChainCompaction struct {
	TotalEventsCount         int    `json:"total_events_count"`
	RetainedEventsCount      int    `json:"retained_events_count"`
	CompactedContextualCount int    `json:"compacted_contextual_count"`
	RollingSummary           string `json:"rolling_summary,omitempty"`
}

type EventChain struct {
	ID                     string            `json:"id"`
	Status                 EventChainStatus  `json:"status"`
	ActivationID           string            `json:"activation_id,omitempty"`
	SequenceKey            string            `json:"sequence_key,omitempty"`
	StartedAt              time.Time         `json:"started_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
	LastEventAt            time.Time         `json:"last_event_at"`
	LastSignificantEventAt time.Time         `json:"last_significant_event_at"`
	ClosedAt               *time.Time        `json:"closed_at,omitempty"`
	ClosedReason           string            `json:"closed_reason,omitempty"`
	PrimaryDeviceID        string            `json:"primary_device_id,omitempty"`
	PrimaryNodeID          string            `json:"primary_node_id,omitempty"`
	ResidentID             string            `json:"resident_id,omitempty"`
	IdentityID             string            `json:"identity_id,omitempty"`
	TrackIDs               []string          `json:"track_ids,omitempty"`
	ClipIDs                []string          `json:"clip_ids,omitempty"`
	CurrentState           string            `json:"current_state,omitempty"`
	DangerLevel            DangerLevel       `json:"danger_level"`
	DangerScore            float64           `json:"danger_score"`
	MaxDangerLevel         DangerLevel       `json:"max_danger_level"`
	MaxDangerScore         float64           `json:"max_danger_score"`
	DangerReasons          []string          `json:"danger_reasons,omitempty"`
	Title                  string            `json:"title,omitempty"`
	Summary                string            `json:"summary,omitempty"`
	EventsCount            int               `json:"events_count"`
	SignificantEventsCount int               `json:"significant_events_count"`
	ContextualEventsCount  int               `json:"contextual_events_count"`
	MotionCount            int               `json:"motion_count"`
	RecentEvents           []PublicEvent     `json:"recent_events,omitempty"`
	Evaluations            []ChainEvaluation `json:"evaluations,omitempty"`
	RollingSummary         string            `json:"rolling_summary,omitempty"`
	Compaction             *ChainCompaction  `json:"compaction,omitempty"`
	SignificantEventTypes  []string          `json:"significant_event_types,omitempty"`
	Critical               bool              `json:"critical,omitempty"`
	Simulated              bool              `json:"simulated,omitempty"`
	TestRunID              string            `json:"test_run_id,omitempty"`
	ScenarioID             string            `json:"scenario_id,omitempty"`
	CreatedBy              string            `json:"created_by,omitempty"`
}

type CriticalChainMemory struct {
	ID                    string    `json:"id"`
	TemplateID            string    `json:"template_id"`
	FirstSeen             time.Time `json:"first_seen"`
	LastSeen              time.Time `json:"last_seen"`
	Occurrences           int       `json:"occurrences"`
	MaxDangerLevel        string    `json:"max_danger_level"`
	MaxDangerScore        float64   `json:"max_danger_score"`
	RepresentativeChainID string    `json:"representative_chain_id"`
	RecentChainIDs        []string  `json:"recent_chain_ids"`
	SignificantEventTypes []string  `json:"significant_event_types"`
	NodePattern           []string  `json:"node_pattern"`
	DeviceTypes           []string  `json:"device_types"`
	IdentityPattern       []string  `json:"identity_pattern"`
	TypicalStatePath      []string  `json:"typical_state_path"`
	TypicalDangerPath     []string  `json:"typical_danger_path"`
	Summary               string    `json:"summary,omitempty"`
	LearnedReason         string    `json:"learned_reason,omitempty"`
	RecommendedActions    []string  `json:"recommended_actions"`
	ActionsTaken          []string  `json:"actions_taken"`
	Outcomes              []string  `json:"outcomes"`
	Confidence            float64   `json:"confidence"`
	FeedbackCount         int       `json:"feedback_count,omitempty"`
	LastFeedbackAt        time.Time `json:"last_feedback_at,omitempty"`
	Simulated             bool      `json:"simulated,omitempty"`
	Source                string    `json:"source"`
	SimulatedOccurrences  int       `json:"simulated_occurrences"`
	RealOccurrences       int       `json:"real_occurrences"`
}

func NormalizeCriticalChainMemory(memory CriticalChainMemory) CriticalChainMemory {
	if memory.MaxDangerLevel != string(DangerNone) && memory.MaxDangerLevel != string(DangerLow) && memory.MaxDangerLevel != string(DangerMedium) && memory.MaxDangerLevel != string(DangerHigh) && memory.MaxDangerLevel != string(DangerCritical) {
		memory.MaxDangerLevel = string(DangerNone)
	}
	if memory.MaxDangerScore < 0 {
		memory.MaxDangerScore = 0
	}
	if memory.Confidence < 0 {
		memory.Confidence = 0
	}
	if memory.Confidence > 1 {
		memory.Confidence = 1
	}
	if memory.Occurrences < 0 {
		memory.Occurrences = 0
	}
	if memory.FeedbackCount < 0 {
		memory.FeedbackCount = 0
	}
	if memory.SimulatedOccurrences < 0 {
		memory.SimulatedOccurrences = 0
	}
	if memory.RealOccurrences < 0 {
		memory.RealOccurrences = 0
	}
	if memory.SimulatedOccurrences == 0 && memory.RealOccurrences == 0 && memory.Occurrences > 0 {
		memory.RealOccurrences = memory.Occurrences
	}
	switch memory.Source {
	case "simulation", "real", "mixed":
	default:
		memory.Source = "real"
	}
	if memory.SimulatedOccurrences > 0 && memory.RealOccurrences > 0 {
		memory.Source = "mixed"
	} else if memory.SimulatedOccurrences > 0 {
		memory.Source = "simulation"
	} else if memory.RealOccurrences > 0 {
		memory.Source = "real"
	}
	memory.Simulated = memory.Source == "simulation"
	if memory.RecentChainIDs == nil {
		memory.RecentChainIDs = []string{}
	}
	if memory.SignificantEventTypes == nil {
		memory.SignificantEventTypes = []string{}
	}
	if memory.NodePattern == nil {
		memory.NodePattern = []string{}
	}
	if memory.DeviceTypes == nil {
		memory.DeviceTypes = []string{}
	}
	if memory.IdentityPattern == nil {
		memory.IdentityPattern = []string{}
	}
	if memory.TypicalStatePath == nil {
		memory.TypicalStatePath = []string{}
	}
	if memory.TypicalDangerPath == nil {
		memory.TypicalDangerPath = []string{}
	}
	if memory.RecommendedActions == nil {
		memory.RecommendedActions = []string{}
	}
	if memory.ActionsTaken == nil {
		memory.ActionsTaken = []string{}
	}
	if memory.Outcomes == nil {
		memory.Outcomes = []string{}
	}
	return memory
}
