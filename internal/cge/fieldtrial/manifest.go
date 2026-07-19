package fieldtrial

import "time"

type SessionStatus string

const (
	SessionOpen      SessionStatus = "open"
	SessionClosed    SessionStatus = "closed"
	SessionRecovered SessionStatus = "recovered"
	SessionDegraded  SessionStatus = "degraded"
)

type ConfigurationSnapshot struct {
	ContextEnabled       bool   `json:"context_enabled"`
	RoutineLearning      bool   `json:"routine_learning_enabled"`
	DeviationEnabled     bool   `json:"deviation_enabled"`
	CognitiveShadow      bool   `json:"cognitive_shadow_enabled"`
	ContextSchemaVersion string `json:"context_schema_version"`
	RoutinePolicyVersion string `json:"routine_policy_version"`
	DeviationPolicy      string `json:"deviation_policy_version"`
}

type PolicyVersions struct {
	Association string `json:"association"`
	Evidence    string `json:"evidence"`
	Context     string `json:"context"`
	Routines    string `json:"routines"`
	Deviation   string `json:"deviation"`
}

type SessionManifest struct {
	SchemaVersion string `json:"schema_version"`
	SessionID     string `json:"session_id"`

	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`

	HostArchitecture string `json:"host_architecture"`

	CGEConfiguration                  ConfigurationSnapshot `json:"cge_configuration"`
	PolicyVersions                    PolicyVersions        `json:"policy_versions"`
	CognitiveConfigurationFingerprint string                `json:"cognitive_configuration_fingerprint,omitempty"`

	ContextSchemaVersion   string `json:"context_schema_version"`
	RoutinePolicyVersion   string `json:"routine_policy_version"`
	DeviationPolicyVersion string `json:"deviation_policy_version"`

	FirstSequence uint64 `json:"first_sequence"`
	LastSequence  uint64 `json:"last_sequence"`

	SegmentCount int `json:"segment_count"`

	EventCount      uint64 `json:"event_count"`
	AnnotationCount uint64 `json:"annotation_count"`

	LastSegmentHash string        `json:"last_segment_hash"`
	Status          SessionStatus `json:"status"`
}

type OpenMetadata struct {
	CGEConfiguration                  ConfigurationSnapshot
	PolicyVersions                    PolicyVersions
	CognitiveConfigurationFingerprint string
}

type ManifestSnapshot struct {
	Manifest  SessionManifest
	Directory string
}
