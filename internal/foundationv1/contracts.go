// Package foundationv1 defines software-only V1 contracts for capabilities
// that are not yet connected to production peripherals or qualified inputs.
package foundationv1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

type FunctionStatus string

const (
	StatusNotConfigured FunctionStatus = "not_configured"
	StatusUnavailable   FunctionStatus = "unavailable"
	StatusSimulatedTest FunctionStatus = "simulated_test"
	StatusDryRun        FunctionStatus = "dry_run"
	StatusAvailable     FunctionStatus = "available"
	StatusFailed        FunctionStatus = "failed"
)

func (s FunctionStatus) Valid() bool {
	switch s {
	case StatusNotConfigured, StatusUnavailable, StatusSimulatedTest, StatusDryRun, StatusAvailable, StatusFailed:
		return true
	default:
		return false
	}
}

// InitialStatus is deliberately conservative: interfaces exist, integrations do not.
type InitialStatus struct {
	CameraHealth FunctionStatus `json:"camera_health"`
	Peripherals  FunctionStatus `json:"peripheral_registry"`
	Action       FunctionStatus `json:"action_execution"`
	Voice        FunctionStatus `json:"voice_communication"`
	Search       FunctionStatus `json:"search_tracking"`
	Topology     FunctionStatus `json:"topology_correlation"`
	Discovery    FunctionStatus `json:"discovery_port"`
	Core         FunctionStatus `json:"core_port"`
	MLP          FunctionStatus `json:"mlp"`
	SafetyGate   FunctionStatus `json:"safety_gate"`
	Store        FunctionStatus `json:"universal_store"`
	Executor     FunctionStatus `json:"dry_run_executor"`
	API          FunctionStatus `json:"state_api"`
}

func DefaultInitialStatus() InitialStatus {
	return InitialStatus{
		CameraHealth: StatusNotConfigured, Peripherals: StatusNotConfigured, Action: StatusNotConfigured,
		Voice: StatusNotConfigured, Search: StatusNotConfigured, Topology: StatusNotConfigured,
		Discovery: StatusNotConfigured, Core: StatusNotConfigured, MLP: StatusNotConfigured,
		SafetyGate: StatusNotConfigured, Store: StatusNotConfigured, Executor: StatusNotConfigured,
		API: StatusNotConfigured,
	}
}

type CameraHealth struct {
	Status       FunctionStatus   `json:"status"`
	Availability string           `json:"availability"` // online, offline, unknown
	Stream       string           `json:"stream"`       // flowing, absent, frozen, unknown
	Tamper       string           `json:"tamper"`       // none, suspected, unknown
	Degraded     bool             `json:"degraded"`
	SceneSafety  string           `json:"scene_safety"` // always unknown when camera unavailable
	ObservedAt   time.Time        `json:"observed_at,omitempty"`
	Evidence     RedactedEvidence `json:"evidence"`
}

type RedactedEvidence struct {
	SchemaVersion string `json:"schema_version"`
	Source        string `json:"source"`
	Signal        string `json:"signal"`
	Confidence    string `json:"confidence"` // coarse bucket, never raw score
	Redacted      bool   `json:"redacted"`
}

type Peripheral struct {
	ID           string            `json:"id"`
	Type         string            `json:"type"`
	Zone         string            `json:"zone"`
	Status       FunctionStatus    `json:"status"`
	State        string            `json:"state"` // known, unknown
	Capabilities []CapabilityState `json:"capabilities"`
}

type CapabilityState struct {
	Name  string         `json:"name"`
	State string         `json:"state"` // available, unavailable, unknown
	Mode  FunctionStatus `json:"mode"`
}

type ActionProposal struct {
	RequestID  string `json:"request_id"`
	EpisodeID  string `json:"episode_id,omitempty"`
	Action     string `json:"action"`
	Capability string `json:"capability"`
	Zone       string `json:"zone"`
}

type ActionResult struct {
	RequestID              string         `json:"request_id"`
	Status                 string         `json:"status"` // blocked, dry_run, duplicate, unavailable
	Reason                 string         `json:"reason,omitempty"`
	PeripheralState        string         `json:"peripheral_state"` // unknown unless feedback exists
	PhysicalActionExecuted bool           `json:"physical_action_executed"`
	FunctionStatus         FunctionStatus `json:"function_status"`
}

type CommunicationIntent struct {
	ID        string        `json:"id"`
	Zones     []string      `json:"zones"`
	Priority  string        `json:"priority"`
	Cooldown  time.Duration `json:"cooldown_ns"`
	Recipient string        `json:"recipient"` // abstract role, not a person identity
	TextKey   string        `json:"text_key"`  // symbolic key; no rendered speech or free-form sensitive text
}

type CommunicationRequest struct {
	IntentID      string         `json:"intent_id"`
	Zones         []string       `json:"zones"`
	Priority      string         `json:"priority"`
	Cooldown      time.Duration  `json:"cooldown_ns"`
	Recipient     string         `json:"recipient"`
	TextKey       string         `json:"text_key"`
	Status        string         `json:"status"` // queued_dry_run, suppressed
	Reason        string         `json:"reason,omitempty"`
	TTSStatus     FunctionStatus `json:"tts_status"`
	AudioRendered bool           `json:"audio_rendered"`
}

type SearchQuery struct {
	ID       string    `json:"id"`
	Zones    []string  `json:"zones"`
	Target   string    `json:"target"` // abstract category only
	IssuedAt time.Time `json:"issued_at"`
}

type SearchState struct {
	Status              FunctionStatus       `json:"status"`
	QueryID             string               `json:"query_id"`
	Occupancy           string               `json:"occupancy"`   // unknown, occupied, unoccupied
	Coverage            string               `json:"coverage"`    // unknown, partial, complete
	Association         string               `json:"association"` // none, ambiguous, single_observation
	LastObservation     *RedactedObservation `json:"last_observation,omitempty"`
	ExpiresAt           time.Time            `json:"expires_at,omitempty"`
	CrossCameraIdentity bool                 `json:"cross_camera_identity"`
	PTZUsed             bool                 `json:"ptz_used"`
}

type RedactedObservation struct {
	ID         string    `json:"id"`
	Zone       string    `json:"zone"`
	Category   string    `json:"category"`
	Confidence string    `json:"confidence"`
	ObservedAt time.Time `json:"observed_at"`
	Redacted   bool      `json:"redacted"`
}

type Zone struct {
	ID string `json:"id"`
}

type Transition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Topology struct {
	Status      FunctionStatus `json:"status"`
	Zones       []Zone         `json:"zones"`
	Connections []Transition   `json:"allowed_transitions"`
}

type Correlation struct {
	ID             string   `json:"id"`
	EpisodeID      string   `json:"episode_id"`
	ObservationIDs []string `json:"observation_ids"`
	Confidence     string   `json:"confidence"`
	Classification string   `json:"classification"` // hypothesis, observed, ambiguous
	EvidenceRef    string   `json:"evidence_ref,omitempty"`
	Biometric      bool     `json:"biometric_association"`
}

type Record struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Status    FunctionStatus `json:"status"`
	Payload   any            `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

// StorePort is implemented by the Core-owned Universal Store adapter.
type StorePort interface {
	PutFoundation(context.Context, Record) (uint64, error)
	FoundationSnapshot(context.Context) ([]Record, uint64, error)
}

// DiscoveryPort validates and redacts before any evidence is persisted.
type DiscoveryPort interface {
	Accept(context.Context, Record) (Record, error)
}

type MLPPort interface {
	Propose(context.Context, Record) (ActionProposal, error)
}

type CoreOutput struct {
	Proposal      ActionProposal
	Decision      string
	Allowed       bool
	StoreRevision uint64
	ModelStatus   FunctionStatus
}

// CorePort is the integration seam around the existing cognitive Core.
type CorePort interface {
	Process(context.Context, Record, MLPPort, GatePort) (CoreOutput, error)
}

type GatePort interface {
	Allow(context.Context, ActionProposal) (bool, string, error)
}

// ExecutorPort is intentionally named and specified as dry-run only.
type ExecutorPort interface {
	ExecuteDryRun(context.Context, ActionProposal) (ActionResult, error)
}

var ErrForbiddenProjection = errors.New("state projection contains forbidden data")

// StateSnapshot is an allowlisted API shape. It has no media, raw identifiers,
// biometric identity, plate, secret, or local path fields.
type StateSnapshot struct {
	SchemaVersion          string                `json:"schema_version"`
	Revision               uint64                `json:"revision"`
	Functions              InitialStatus         `json:"functions"`
	Camera                 CameraHealth          `json:"camera"`
	Peripherals            []Peripheral          `json:"peripherals"`
	LastAction             *ActionResult         `json:"last_action,omitempty"`
	Communication          *CommunicationRequest `json:"communication,omitempty"`
	Search                 *SearchState          `json:"search,omitempty"`
	Topology               Topology              `json:"topology"`
	Correlations           []Correlation         `json:"correlations"`
	PhysicalActionExecuted bool                  `json:"physical_action_executed"`
	AudioRendered          bool                  `json:"audio_rendered"`
}

// StateHandler enforces authorization at the HTTP boundary, independently of
// any caller-side visibility filtering.
func StateHandler(snapshot func() StateSnapshot, authorize func(*http.Request) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if authorize == nil || !authorize(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if snapshot == nil {
			http.Error(w, "state unavailable", http.StatusServiceUnavailable)
			return
		}
		state := snapshot()
		state.SchemaVersion = "synora.foundation-state/v1"
		if err := ValidatePublicState(state); err != nil {
			http.Error(w, "state projection unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(state)
	})
}
