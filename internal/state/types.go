package state

import "time"

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
	ID         string    `json:"id"`
	NodeID     string    `json:"node_id,omitempty"`
	Online     bool      `json:"online"`
	LastSeen   time.Time `json:"last_seen"`
	LastClipID string    `json:"last_clip_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
	ID         string    `json:"id"`
	ResidentID string    `json:"resident_id"`
	Location   string    `json:"location,omitempty"`
	Confidence float64   `json:"confidence"`
	State      string    `json:"state,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastSeen   time.Time `json:"last_seen"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type ClipState struct {
	ID        string    `json:"id"`
	CameraID  string    `json:"camera_id"`
	EventID   string    `json:"event_id,omitempty"`
	Path      string    `json:"path,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SystemState struct {
	LastState            string            `json:"last_state"`
	LastStateTime        time.Time         `json:"last_state_time"`
	PreviousState        string            `json:"previous_state,omitempty"`
	DangerLevel          string            `json:"danger_level"`
	DangerScore          float64           `json:"danger_score"`
	DangerKnown          bool              `json:"danger_known"`
	DangerSource         string            `json:"danger_source"`
	Armed                bool              `json:"armed"`
	Degraded             bool              `json:"degraded"`
	DegradationReasons   []string          `json:"degradation_reasons"`
	RuntimeComponents    map[string]string `json:"runtime_components"`
	RuntimeComponentInfo map[string]string `json:"runtime_component_info,omitempty"`
	RuntimeModels        map[string]string `json:"runtime_models"`
	LastRealEventAt      time.Time         `json:"last_real_event_at,omitempty"`
	LastActionRequestAt  time.Time         `json:"last_action_request_at,omitempty"`
	LastActionAt         time.Time         `json:"last_action_at,omitempty"`
	BlockingReasons      []string          `json:"blocking_reasons"`
	BlockedActionsRecent []map[string]any  `json:"blocked_actions_recent"`
	ManualRiskActive     bool              `json:"manual_risk_active"`
	ManualRiskTest       bool              `json:"manual_risk_test"`
	ManualRiskLevel      string            `json:"manual_risk_level,omitempty"`
	ManualRiskScore      float64           `json:"manual_risk_score,omitempty"`
	ManualRiskExpiresAt  time.Time         `json:"manual_risk_expires_at,omitempty"`
	IntrusionActive      bool              `json:"intrusion_active"`
	IntrusionTime        time.Time         `json:"intrusion_time"`
	EmergencyActive      bool              `json:"emergency_active"`
	EmergencyTime        time.Time         `json:"emergency_time"`
}

type ExpirationConfig struct {
	Tracks     time.Duration
	Clusters   time.Duration
	Identities time.Duration
	Presence   time.Duration
	Clips      time.Duration
	Windows    time.Duration
}

// DefaultPresenceTTL keeps a resident present long enough for normal camera
// gaps while retaining last_seen after expiration.
const DefaultPresenceTTL = 15 * time.Minute

type CleanupResult struct {
	Deleted map[string][]string `json:"deleted"`
}

func DefaultExpirationConfig() ExpirationConfig {
	return ExpirationConfig{
		Tracks:     20 * time.Second,
		Clusters:   15 * time.Second,
		Identities: 45 * time.Second,
		Presence:   DefaultPresenceTTL,
		Clips:      5 * time.Minute,
		Windows:    20 * time.Second,
	}
}
