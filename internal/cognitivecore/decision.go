package cognitivecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	DecisionSchemaVersion = "cognitive-decision/v1"
	BundleManifestSchema  = "synora.cognitive-v1-manifest/v1"
)

var ErrModelUnavailable = errors.New("V1 MLP model unavailable")

var HeadOrder = [...]string{"danger", "incident", "task", "action"}

var AbstractActions = [...]string{"no_action", "notify", "record", "light", "lock", "siren", "request_review"}

type ActionIntent struct {
	Action     string `json:"action"`
	Topology   string `json:"topology,omitempty"`
	Capability string `json:"capability,omitempty"`
}

type MLPOutput struct {
	DangerLabel string       `json:"danger_label"`
	DangerScore float32      `json:"danger_score"`
	Incidents   []string     `json:"incidents,omitempty"`
	Task        string       `json:"task,omitempty"`
	Action      ActionIntent `json:"action"`
}

type Decision struct {
	SchemaVersion  string             `json:"schema_version"`
	Status         string             `json:"status"`
	Mode           string             `json:"mode"`
	Source         string             `json:"source"`
	HeadOrder      []string           `json:"head_order"`
	InputDimension int                `json:"input_dimension"`
	DangerLabel    string             `json:"danger_label,omitempty"`
	DangerScore    float32            `json:"danger_score,omitempty"`
	Incidents      []string           `json:"incidents,omitempty"`
	Task           string             `json:"task,omitempty"`
	Action         ActionAssessment   `json:"action"`
	HeadLatencyMS  map[string]float64 `json:"head_latency_ms,omitempty"`
	Error          string             `json:"error,omitempty"`
	GeneratedAt    time.Time          `json:"generated_at"`
}

type ActionAssessment struct {
	Proposed               ActionIntent `json:"proposed"`
	Status                 string       `json:"status"`
	Reasons                []string     `json:"reasons,omitempty"`
	PhysicalActionExecuted bool         `json:"physical_action_executed"`
}

type MLPBackend interface {
	Run(context.Context, EncodedSnapshot, CognitiveSnapshot) (MLPOutput, map[string]float64, error)
}

type UnavailableMLP struct{ Reason string }

func (u UnavailableMLP) Run(context.Context, EncodedSnapshot, CognitiveSnapshot) (MLPOutput, map[string]float64, error) {
	if u.Reason == "" {
		return MLPOutput{}, nil, ErrModelUnavailable
	}
	return MLPOutput{}, nil, fmt.Errorf("%w: %s", ErrModelUnavailable, u.Reason)
}

type Manifest struct {
	SchemaVersion  string              `json:"schema_version"`
	SnapshotSchema string              `json:"snapshot_schema"`
	EncoderSchema  string              `json:"encoder_schema"`
	InputDimension int                 `json:"input_dimension"`
	Labels         map[string][]string `json:"labels"`
	Heads          []string            `json:"heads"`
	WeightsSHA256  string              `json:"weights_sha256"`
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != BundleManifestSchema || m.SnapshotSchema != SnapshotSchemaVersion || m.EncoderSchema != EncoderSchemaVersion || m.InputDimension != CognitiveVectorSize {
		return fmt.Errorf("incompatible V1 MLP manifest")
	}
	if len(m.Heads) != len(HeadOrder) {
		return fmt.Errorf("V1 MLP manifest head count mismatch")
	}
	for i, head := range HeadOrder {
		if m.Heads[i] != head {
			return fmt.Errorf("V1 MLP head order mismatch at %d", i)
		}
		if len(m.Labels[head]) == 0 {
			return fmt.Errorf("V1 MLP labels missing for %s", head)
		}
	}
	return nil
}

func LoadManifest(path string) (Manifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
	}
	return manifest, nil
}

type SafetyGate struct {
	DryRun            bool
	Capabilities      map[string]bool
	AllowedTopologies map[string]bool
	LastAction        map[string]time.Time
	Cooldown          time.Duration
}

func (g SafetyGate) Apply(output MLPOutput, snapshot CognitiveSnapshot, now time.Time) ActionAssessment {
	assessment := ActionAssessment{Proposed: output.Action, Status: "not_requested", PhysicalActionExecuted: false}
	if output.Action.Action == "" {
		assessment.Proposed.Action = "no_action"
	}
	if assessment.Proposed.Action == "no_action" {
		return assessment
	}
	if !isAbstractAction(assessment.Proposed.Action) {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"action_not_in_abstract_catalog"}
		return assessment
	}
	if g.DryRun {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"dry_run"}
		return assessment
	}
	if !contractTopologyAllowed(g.AllowedTopologies, snapshot.Topology) {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"topology_not_allowed"}
		return assessment
	}
	if assessment.Proposed.Capability == "" || !g.Capabilities[assessment.Proposed.Capability] {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"capability_unavailable"}
		return assessment
	}
	if g.Cooldown > 0 && g.LastAction != nil {
		if at, ok := g.LastAction[assessment.Proposed.Action]; ok && now.Sub(at) < g.Cooldown {
			assessment.Status = "blocked"
			assessment.Reasons = []string{"cooldown"}
			return assessment
		}
	}
	assessment.Status = "allowed"
	return assessment
}

func isAbstractAction(value string) bool {
	for _, action := range AbstractActions {
		if action == value {
			return true
		}
	}
	return false
}
func contractTopologyAllowed(values map[string]bool, topology string) bool {
	if len(values) == 0 {
		return false
	}
	return values[topology]
}

func makeUnavailableDecision(now time.Time, err error) Decision {
	return Decision{SchemaVersion: DecisionSchemaVersion, Status: "unavailable", Mode: "active_dry_run", Source: "mlp", HeadOrder: append([]string(nil), HeadOrder[:]...), InputDimension: CognitiveVectorSize, Action: ActionAssessment{Proposed: ActionIntent{Action: "no_action"}, Status: "not_requested", PhysicalActionExecuted: false}, Error: err.Error(), GeneratedAt: now.UTC()}
}
