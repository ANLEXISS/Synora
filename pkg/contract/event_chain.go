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
	RecentChainIDs        []string  `json:"recent_chain_ids,omitempty"`
	SignificantEventTypes []string  `json:"significant_event_types,omitempty"`
	NodePattern           []string  `json:"node_pattern,omitempty"`
	DeviceTypes           []string  `json:"device_types,omitempty"`
	IdentityPattern       []string  `json:"identity_pattern,omitempty"`
	TypicalStatePath      []string  `json:"typical_state_path,omitempty"`
	TypicalDangerPath     []string  `json:"typical_danger_path,omitempty"`
	Summary               string    `json:"summary,omitempty"`
	LearnedReason         string    `json:"learned_reason,omitempty"`
	RecommendedActions    []string  `json:"recommended_actions,omitempty"`
	ActionsTaken          []string  `json:"actions_taken,omitempty"`
	Outcomes              []string  `json:"outcomes,omitempty"`
	Confidence            float64   `json:"confidence"`
	FeedbackCount         int       `json:"feedback_count,omitempty"`
	LastFeedbackAt        time.Time `json:"last_feedback_at,omitempty"`
}
