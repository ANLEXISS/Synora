package state

import (
	"math"
	"time"

	"synora/pkg/contract"
)

type DeviceState struct {
	ID            string    `json:"id"`
	Type          string    `json:"type,omitempty"`
	Role          string    `json:"role,omitempty"`
	Room          string    `json:"room,omitempty"`
	NodeID        string    `json:"node_id,omitempty"`
	Online        bool      `json:"online"`
	LastSeen      time.Time `json:"last_seen"`
	LastEventID   string    `json:"last_event_id,omitempty"`
	ActivityCount int       `json:"activity_count"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CameraState struct {
	ID            string    `json:"id"`
	NodeID        string    `json:"node_id,omitempty"`
	Endpoint      string    `json:"endpoint,omitempty"`
	HardwareID    string    `json:"hardware_id,omitempty"`
	Firmware      string    `json:"firmware,omitempty"`
	Capabilities  []string  `json:"capabilities,omitempty"`
	ObservationID string    `json:"observation_id,omitempty"`
	Online        bool      `json:"online"`
	LastSeen      time.Time `json:"last_seen"`
	LastClipID    string    `json:"last_clip_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NodeState struct {
	NodeID      string    `json:"node_id"`
	DangerScore float64   `json:"danger_score"`
	LastEventID string    `json:"last_event_id,omitempty"`
	LastSeen    time.Time `json:"last_seen"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Track struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"device_id,omitempty"`
	NodeID     string    `json:"node_id,omitempty"`
	Type       string    `json:"type,omitempty"`
	Identity   string    `json:"identity,omitempty"`
	Confidence float64   `json:"confidence"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastSeen   time.Time `json:"last_seen"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// ResidentTrack is the durable Core-side association keyed only by resident_id.
// It contains no biometric material and is never sourced from display_name.
type ResidentTrack struct {
	ResidentID   string    `json:"resident_id"`
	LastNodeID   string    `json:"last_node_id,omitempty"`
	LastDeviceID string    `json:"last_device_id,omitempty"`
	LastTrackID  string    `json:"last_track_id,omitempty"`
	LastEventID  string    `json:"last_event_id,omitempty"`
	ActivationID string    `json:"activation_id,omitempty"`
	SequenceKey  string    `json:"sequence_key,omitempty"`
	Epoch        string    `json:"epoch,omitempty"`
	Confidence   float64   `json:"confidence"`
	LastSeen     time.Time `json:"last_seen"`
	UpdatedAt    time.Time `json:"updated_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// EntityTrack is the anonymous track projection used until a later valid
// vision.identity binds the same track to a resident_id.
type EntityTrack struct {
	ID                  string    `json:"id"`
	TrackID             string    `json:"track_id,omitempty"`
	NodeID              string    `json:"node_id,omitempty"`
	DeviceID            string    `json:"device_id,omitempty"`
	ActivationID        string    `json:"activation_id,omitempty"`
	SequenceKey         string    `json:"sequence_key,omitempty"`
	Epoch               string    `json:"epoch,omitempty"`
	ResidentID          string    `json:"resident_id,omitempty"`
	CandidateResidentID string    `json:"-"`
	Kind                string    `json:"kind"`
	Confidence          float64   `json:"confidence"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
	LastSeen            time.Time `json:"last_seen"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type Cluster struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"node_id,omitempty"`
	Type      string    `json:"type"`
	Score     float64   `json:"score"`
	EventIDs  []string  `json:"event_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type IdentityState struct {
	ID           string    `json:"id"`
	LastNodeID   string    `json:"last_node_id,omitempty"`
	LastDeviceID string    `json:"last_device_id,omitempty"`
	Confidence   float64   `json:"confidence"`
	State        string    `json:"state,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastSeen     time.Time `json:"last_seen"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type PresenceState struct {
	ID               string    `json:"id"`
	ResidentID       string    `json:"resident_id"`
	Location         string    `json:"location,omitempty"`
	Confidence       float64   `json:"confidence"`
	ConfidenceSource string    `json:"confidence_source,omitempty"`
	State            string    `json:"state,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	LastSeen         time.Time `json:"last_seen"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type ClipState = contract.Clip

type SystemState struct {
	LastState     string    `json:"last_state"`
	LastStateTime time.Time `json:"last_state_time"`
	// Lifecycle fields are owned by Core's recovery gate. Ready and Healthy
	// are never inferred from process liveness; they are evidence-backed views
	// of required dependency recovery.
	LifecycleState     string    `json:"lifecycle_state"`
	LifecycleReason    string    `json:"lifecycle_reason,omitempty"`
	LifecycleUpdatedAt time.Time `json:"lifecycle_updated_at,omitempty"`
	Ready              bool      `json:"ready"`
	Healthy            bool      `json:"healthy"`
	RecoveryComplete   bool      `json:"recovery_complete"`
	PreviousState      string    `json:"previous_state,omitempty"`
	DangerLevel        string    `json:"danger_level"`
	DangerScore        float64   `json:"danger_score"`
	DangerKnown        bool      `json:"danger_known"`
	DangerSource       string    `json:"danger_source"`
	// DangerScoreCurrent is the decayed, runtime score. DangerScore remains
	// the historical/current compatibility field used by older clients.
	DangerDecayEnabled         bool                       `json:"danger_decay_enabled"`
	DangerDecay                map[string]any             `json:"danger_decay,omitempty"`
	DangerDecayLastTick        time.Time                  `json:"danger_decay_last_tick,omitempty"`
	DangerDecayWindowMinutes   int                        `json:"danger_decay_window_minutes,omitempty"`
	DangerDecayHalfLifeMinutes int                        `json:"danger_decay_half_life_minutes,omitempty"`
	DangerScoreCurrent         float64                    `json:"danger_score_current"`
	DangerScorePeak            float64                    `json:"danger_score_peak"`
	DangerScoreUpdatedAt       time.Time                  `json:"danger_score_updated_at,omitempty"`
	DangerReasonsCurrent       []string                   `json:"danger_reasons_current,omitempty"`
	DangerDecayDebug           map[string]any             `json:"danger_decay_debug,omitempty"`
	Armed                      bool                       `json:"armed"`
	Degraded                   bool                       `json:"degraded"`
	DegradationReasons         []string                   `json:"degradation_reasons"`
	RuntimeComponents          map[string]string          `json:"runtime_components"`
	RuntimeComponentInfo       map[string]string          `json:"runtime_component_info,omitempty"`
	RuntimeModels              map[string]string          `json:"runtime_models"`
	LastRealEventAt            time.Time                  `json:"last_real_event_at,omitempty"`
	LastActionRequestAt        time.Time                  `json:"last_action_request_at,omitempty"`
	LastActionAt               time.Time                  `json:"last_action_at,omitempty"`
	BlockingReasons            []string                   `json:"blocking_reasons"`
	BlockedActionsRecent       []map[string]any           `json:"blocked_actions_recent"`
	ManualRiskActive           bool                       `json:"manual_risk_active"`
	ManualRiskTest             bool                       `json:"manual_risk_test"`
	ManualRiskLevel            string                     `json:"manual_risk_level,omitempty"`
	ManualRiskScore            float64                    `json:"manual_risk_score,omitempty"`
	ManualRiskExpiresAt        time.Time                  `json:"manual_risk_expires_at,omitempty"`
	IntrusionActive            bool                       `json:"intrusion_active"`
	IntrusionTime              time.Time                  `json:"intrusion_time"`
	EmergencyActive            bool                       `json:"emergency_active"`
	EmergencyTime              time.Time                  `json:"emergency_time"`
	Security                   contract.SecurityModeState `json:"security"`
}

type ExpirationConfig struct {
	Tracks     time.Duration
	Clusters   time.Duration
	Identities time.Duration
	Presence   time.Duration
	Clips      time.Duration
	Windows    time.Duration
}

// ContextSourceSnapshot is the intentionally small read-only state slice
// consumed by Core context adapters. It contains no mutable maps or pointers
// into Store and no action, validation, or event collections.
type ContextSourceSnapshot struct {
	// Revision is the monotonic StateStore revision captured with the facts.
	// It is metadata for read-only consumers and does not grant them authority.
	Revision uint64
	Devices  []DeviceState
	Cameras  []CameraState
	Presence []PresenceState
	System   ContextSystemState
}

// ContextSystemState is the bounded system view exposed to read-only context
// adapters. It deliberately excludes the mutable maps, action history, and
// security details carried by SystemState.
type ContextSystemState struct {
	LastState    string
	Armed        bool
	SecurityMode string
}

// DefaultPresenceTTL keeps a resident present long enough for normal camera
// gaps while retaining last_seen after expiration.
const DefaultPresenceTTL = 15 * time.Minute

// PresenceDecayTau is the confidence time constant. It is intentionally
// independent from track/cluster cleanup TTLs and from the durable presence
// record retention policy.
const PresenceDecayTau = 15 * time.Minute

// DecayedPresenceConfidence returns confidence(t) = confidence(0) *
// exp(-elapsed/tau). Capture confidence remains the source evidence; this is
// only the time-qualified runtime value.
func DecayedPresenceConfidence(confidence float64, elapsed time.Duration) float64 {
	if confidence <= 0 || elapsed <= 0 {
		if confidence < 0 {
			return 0
		}
		if confidence > 1 {
			return 1
		}
		return confidence
	}
	if elapsed == 0 {
		return confidence
	}
	value := confidence * math.Exp(-elapsed.Seconds()/PresenceDecayTau.Seconds())
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

type CleanupResult struct {
	Deleted map[string][]string `json:"deleted"`
}

func DefaultExpirationConfig() ExpirationConfig {
	return ExpirationConfig{
		Tracks:     20 * time.Second,
		Clusters:   10 * time.Second,
		Identities: 45 * time.Second,
		Presence:   DefaultPresenceTTL,
		Clips:      5 * time.Minute,
		Windows:    20 * time.Second,
	}
}
