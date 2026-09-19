package cognitivecore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	EncoderVersion string              `json:"encoder_version"`
	InputDimension int                 `json:"input_dimension"`
	FeatureNames   []string            `json:"feature_names"`
	Labels         map[string][]string `json:"labels"`
	Heads          []string            `json:"heads"`
	WeightsSHA256  string              `json:"weights_sha256"`
	HeadVersions   map[string]string   `json:"head_versions"`
	Artifacts      map[string]Artifact `json:"artifacts"`
}

type Artifact struct {
	Version    string    `json:"version"`
	Artifact   string    `json:"artifact"`
	SHA256     string    `json:"sha256"`
	Labels     []string  `json:"labels"`
	Thresholds []float32 `json:"thresholds"`
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != BundleManifestSchema || m.SnapshotSchema != SnapshotSchemaVersion || m.EncoderSchema != EncoderSchemaVersion || m.EncoderVersion != "1.0.0" || m.InputDimension != CognitiveVectorSize {
		return fmt.Errorf("V1 MLP manifest does not match the contract")
	}
	if len(m.FeatureNames) != CognitiveVectorSize {
		return fmt.Errorf("V1 MLP feature order is missing")
	}
	for i, name := range CognitiveFeatureNames {
		if m.FeatureNames[i] != name {
			return fmt.Errorf("V1 MLP feature order mismatch at %d", i)
		}
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
		if m.HeadVersions != nil && m.HeadVersions[head] != "1.0.0" {
			return fmt.Errorf("V1 MLP head version mismatch for %s", head)
		}
		if m.Artifacts != nil && m.Artifacts[head].Artifact == "" {
			return fmt.Errorf("V1 MLP artifact missing for %s", head)
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

type cpuLayer struct {
	InputSize  int       `json:"input_size"`
	OutputSize int       `json:"output_size"`
	Activation string    `json:"activation"`
	Weights    []float32 `json:"weights"`
	Bias       []float32 `json:"bias"`
}

type cpuHead struct {
	Schema       string     `json:"schema"`
	Head         string     `json:"head"`
	InputSize    int        `json:"input_size"`
	OutputSize   int        `json:"output_size"`
	Labels       []string   `json:"labels"`
	VectorSchema string     `json:"vector_schema"`
	Layers       []cpuLayer `json:"layers"`
}

// CPUBundleMLP is the small deterministic inference backend emitted by the
// V1 Python pipeline. It has no dependency on PyTorch model formats.
type CPUBundleMLP struct {
	Manifest Manifest
	Heads    map[string]cpuHead
}

func LoadCPUBundle(dir string) (*CPUBundleMLP, error) {
	if dir == "" {
		return nil, fmt.Errorf("%w: bundle directory is empty", ErrModelUnavailable)
	}
	manifest, err := LoadManifest(filepath.Join(dir, "MANIFEST.v1.json"))
	if err != nil {
		return nil, err
	}
	bundle := &CPUBundleMLP{Manifest: manifest, Heads: make(map[string]cpuHead, len(HeadOrder))}
	for _, head := range HeadOrder {
		artifact := manifest.Artifacts[head].Artifact
		if artifact == "" {
			artifact = head + ".cpu.json"
		}
		body, err := os.ReadFile(filepath.Join(dir, artifact))
		if err != nil {
			return nil, fmt.Errorf("%w: read %s: %v", ErrModelUnavailable, head, err)
		}
		var model cpuHead
		if err := json.Unmarshal(body, &model); err != nil {
			return nil, fmt.Errorf("%w: decode %s: %v", ErrModelUnavailable, head, err)
		}
		if err := validateCPUHead(model, head, manifest.Labels[head]); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrModelUnavailable, err)
		}
		bundle.Heads[head] = model
	}
	return bundle, nil
}

func validateCPUHead(model cpuHead, expectedHead string, labels []string) error {
	if model.Schema != "synora.cognitive-cpu-mlp/v1" || model.Head != expectedHead || model.InputSize != CognitiveVectorSize || model.OutputSize != len(labels) || model.VectorSchema != EncoderSchemaVersion {
		return fmt.Errorf("invalid CPU head contract for %s", expectedHead)
	}
	if len(model.Labels) != len(labels) {
		return fmt.Errorf("CPU head labels mismatch for %s", expectedHead)
	}
	for i := range labels {
		if model.Labels[i] != labels[i] {
			return fmt.Errorf("CPU head label order mismatch for %s", expectedHead)
		}
	}
	if len(model.Layers) == 0 {
		return fmt.Errorf("CPU head has no layers: %s", expectedHead)
	}
	input := CognitiveVectorSize
	for _, layer := range model.Layers {
		if layer.InputSize != input || layer.OutputSize <= 0 || len(layer.Weights) != layer.InputSize*layer.OutputSize || len(layer.Bias) != layer.OutputSize || (layer.Activation != "relu" && layer.Activation != "identity") {
			return fmt.Errorf("invalid CPU layer for %s", expectedHead)
		}
		input = layer.OutputSize
	}
	if input != model.OutputSize {
		return fmt.Errorf("CPU head output mismatch for %s", expectedHead)
	}
	return nil
}

func (m *CPUBundleMLP) Run(ctx context.Context, encoded EncodedSnapshot, snapshot CognitiveSnapshot) (MLPOutput, map[string]float64, error) {
	if m == nil {
		return MLPOutput{}, nil, ErrModelUnavailable
	}
	if err := encoded.Validate(); err != nil {
		return MLPOutput{}, nil, err
	}
	values := encoded.Values[:]
	latencies := make(map[string]float64, len(HeadOrder))
	outputs := make(map[string][]float32, len(HeadOrder))
	for _, head := range HeadOrder {
		if err := ctx.Err(); err != nil {
			return MLPOutput{}, latencies, err
		}
		started := time.Now()
		result, err := m.runHead(head, values)
		latencies[head] = float64(time.Since(started).Microseconds()) / 1000.0
		if err != nil {
			return MLPOutput{}, latencies, err
		}
		outputs[head] = result
	}
	dangerProb := softmax(outputs["danger"])
	dangerIndex := argmax(dangerProb)
	incidentIndex := argmax(outputs["incident"])
	taskIndex := argmax(outputs["task"])
	actionIndex := argmax(outputs["action"])
	return MLPOutput{DangerLabel: m.Manifest.Labels["danger"][dangerIndex], DangerScore: dangerProb[dangerIndex], Incidents: []string{m.Manifest.Labels["incident"][incidentIndex]}, Task: m.Manifest.Labels["task"][taskIndex], Action: ActionIntent{Action: m.Manifest.Labels["action"][actionIndex], Topology: snapshot.Topology, Capability: m.Manifest.Labels["action"][actionIndex]}}, latencies, nil
}

func (m *CPUBundleMLP) runHead(head string, input []float32) ([]float32, error) {
	model, ok := m.Heads[head]
	if !ok {
		return nil, fmt.Errorf("head %s is unavailable", head)
	}
	current := append([]float32(nil), input...)
	for _, layer := range model.Layers {
		next := make([]float32, layer.OutputSize)
		for output := range next {
			sum := layer.Bias[output]
			for index, value := range current {
				sum += layer.Weights[output*layer.InputSize+index] * value
			}
			if layer.Activation == "relu" && sum < 0 {
				sum = 0
			}
			next[output] = sum
		}
		current = next
	}
	return current, nil
}

func softmax(values []float32) []float32 {
	result := make([]float32, len(values))
	if len(values) == 0 {
		return result
	}
	max := values[0]
	for _, value := range values[1:] {
		if value > max {
			max = value
		}
	}
	var total float32
	for index, value := range values {
		result[index] = float32(math.Exp(float64(value - max)))
		total += result[index]
	}
	if total > 0 {
		for index := range result {
			result[index] /= total
		}
	}
	return result
}
func argmax(values []float32) int {
	index := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[index] {
			index = i
		}
	}
	return index
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
	if len(g.AllowedTopologies) > 0 && !contractTopologyAllowed(g.AllowedTopologies, snapshot.Topology) {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"topology_not_allowed"}
		return assessment
	}
	if len(g.Capabilities) > 0 && (assessment.Proposed.Capability == "" || !g.Capabilities[assessment.Proposed.Capability]) {
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
	if g.DryRun {
		assessment.Status = "allowed_dry_run"
		assessment.Reasons = []string{"dry_run"}
		return assessment
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
