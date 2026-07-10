package contract

import "time"

const (
	DangerCategoryActivity         = "activity"
	DangerCategorySecurity         = "security"
	DangerCategoryIntrusion        = "intrusion"
	DangerCategoryMedicalEmergency = "medical_emergency"
	DangerCategorySystemHealth     = "system_health"
	DangerCategoryDeviceHealth     = "device_health"
	DangerCategoryIdentity         = "identity"
	DangerCategorySimulation       = "simulation"
	DangerCategoryNoise            = "noise"
)

const (
	SystemActionObserve                   = "observe"
	SystemActionStoreEvent                = "store_event"
	SystemActionStoreEvidence             = "store_evidence"
	SystemActionLearnSequence             = "learn_sequence"
	SystemActionUpdatePresence            = "update_presence"
	SystemActionCreateValidation          = "create_validation"
	SystemActionCreateAlert               = "create_alert"
	SystemActionMarkSuspicious            = "mark_suspicious"
	SystemActionMarkIntrusionCandidate    = "mark_intrusion_candidate"
	SystemActionSetIntrusionState         = "set_intrusion_state"
	SystemActionSetEmergencyState         = "set_emergency_state"
	SystemActionRecordClipIfAvailable     = "record_clip_if_available"
	SystemActionIncreaseRetention         = "increase_retention"
	SystemActionLockEvidence              = "lock_evidence"
	SystemActionSuppressNoise             = "suppress_noise"
	SystemActionRequestIdentityAssignment = "request_identity_assignment"
	SystemActionSuggestRuleLater          = "suggest_rule_later"
)

type SystemActionRecommendation struct {
	Type      string         `json:"type"`
	Priority  int            `json:"priority"`
	Reason    string         `json:"reason,omitempty"`
	Target    string         `json:"target,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	DryRun    bool           `json:"dry_run"`
	Simulated bool           `json:"simulated"`
}

type DangerAssessment struct {
	ID                string   `json:"id"`
	EventID           string   `json:"event_id,omitempty"`
	EventType         string   `json:"event_type,omitempty"`
	SequenceSignature string   `json:"sequence_signature,omitempty"`
	TestRunID         string   `json:"test_run_id,omitempty"`
	ScenarioID        string   `json:"scenario_id,omitempty"`
	ScenarioStepID    string   `json:"scenario_step_id,omitempty"`
	EventInstanceID   string   `json:"event_instance_id,omitempty"`
	Level             int      `json:"level"`
	Score             float64  `json:"score"`
	Category          string   `json:"category"`
	Title             string   `json:"title"`
	Explanation       string   `json:"explanation"`
	Reasons           []string `json:"reasons"`
	Evidence          []string `json:"evidence"`

	RecommendedSystemActions []SystemActionRecommendation `json:"recommended_system_actions"`

	ValidationRequired bool   `json:"validation_required"`
	ValidationReason   string `json:"validation_reason,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Simulated bool       `json:"simulated"`
}
