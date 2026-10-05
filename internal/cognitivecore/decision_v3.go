package cognitivecore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var HeadOrderV3 = [...]string{"danger", "incident", "task", "action", "communication_intent"}

var LabelsV3 = map[string][]string{
	"danger":               {"none", "low", "medium", "high", "critical"},
	"incident":             {"none", "routine_presence", "perimeter_presence", "threshold_presence", "interior_intrusion", "anomaly", "action_failure", "technical", "resolved"},
	"task":                 {"monitor", "verify", "notify_security", "contain", "close_incident"},
	"action":               {"no_action", "notify", "record", "light", "lock", "siren", "request_review", "announce"},
	"communication_intent": {"none", "neutral_presence_notice", "request_identity", "wellness_check"},
}

const (
	CommunicationNoneV3                  = "none"
	CommunicationNeutralPresenceNoticeV3 = "neutral_presence_notice"
	CommunicationRequestIdentityV3       = "request_identity"
	CommunicationWellnessCheckV3         = "wellness_check"
)

type CommunicationIntentV3 struct {
	Intent     string `json:"intent"`
	TemplateID string `json:"template_id,omitempty"`
}

type MLPOutputV3 struct {
	DangerLabel             string                `json:"danger_label"`
	DangerScore             float32               `json:"danger_score"`
	Incident                string                `json:"incident"`
	IncidentConfidence      float32               `json:"incident_confidence"`
	Task                    string                `json:"task"`
	TaskConfidence          float32               `json:"task_confidence"`
	Action                  string                `json:"action"`
	ActionConfidence        float32               `json:"action_confidence"`
	Communication           CommunicationIntentV3 `json:"communication_intent"`
	CommunicationConfidence float32               `json:"communication_confidence"`
	ModelVersion            string                `json:"model_version"`
}

type CommunicationAssessmentV3 struct {
	Intent              CommunicationIntentV3 `json:"intent"`
	Capability          string                `json:"capability"`
	Status              string                `json:"status"`
	Reasons             []string              `json:"reasons,omitempty"`
	RenderedText        string                `json:"rendered_text,omitempty"`
	PhysicalAudioPlayed bool                  `json:"physical_audio_played"`
}

type CommunicationTemplate struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Text    string `json:"text"`
}

type DecisionV3 struct {
	SchemaVersion          string                    `json:"schema_version"`
	Status                 string                    `json:"status"`
	Mode                   string                    `json:"mode"`
	Source                 string                    `json:"source"`
	HeadOrder              []string                  `json:"head_order"`
	InputDimension         int                       `json:"input_dimension"`
	DangerLabel            string                    `json:"danger_label,omitempty"`
	DangerScore            float32                   `json:"danger_score,omitempty"`
	Incident               string                    `json:"incident,omitempty"`
	Task                   string                    `json:"task,omitempty"`
	Action                 ActionAssessment          `json:"action"`
	Communication          CommunicationAssessmentV3 `json:"communication"`
	HeadLatencyMS          map[string]float64        `json:"head_latency_ms,omitempty"`
	PhysicalActionExecuted bool                      `json:"physical_action_executed"`
	Error                  string                    `json:"error,omitempty"`
	GeneratedAt            time.Time                 `json:"generated_at"`
}

type MLPBackendV3 interface {
	RunV3(context.Context, EncodedSnapshotV3, CognitiveSnapshotV3) (MLPOutputV3, map[string]float64, error)
}

type V3Manifest struct {
	SchemaVersion          string                       `json:"schema_version"`
	SnapshotSchema         string                       `json:"snapshot_schema"`
	EncoderSchema          string                       `json:"encoder_schema"`
	EncoderVersion         string                       `json:"encoder_version"`
	InputDimension         int                          `json:"input_dimension"`
	HiddenSize             int                          `json:"hidden_size"`
	FeatureNames           []string                     `json:"feature_names"`
	Heads                  []string                     `json:"heads"`
	Labels                 map[string][]string          `json:"labels"`
	Artifacts              map[string]map[string]string `json:"artifacts"`
	Hashes                 map[string]string            `json:"hashes"`
	DryRunRequired         bool                         `json:"dry_run_required"`
	PhysicalActionExecuted bool                         `json:"physical_action_executed"`
	RealVisionMetrics      bool                         `json:"real_vision_metrics"`
	Promoted               bool                         `json:"promoted"`
}

func (m V3Manifest) Validate() error {
	if m.SchemaVersion != "synora.cognitive-v3-manifest/v1" || m.SnapshotSchema != SnapshotSchemaVersionV3 || m.EncoderSchema != EncoderSchemaVersionV3 || m.EncoderVersion != "3.0.0" || m.InputDimension != CognitiveVectorSizeV3 || m.HiddenSize <= 0 || len(m.FeatureNames) != CognitiveVectorSizeV3 {
		return fmt.Errorf("V3 MLP manifest does not match the contract")
	}
	for i, name := range CognitiveFeatureNamesV3 {
		if m.FeatureNames[i] != name {
			return fmt.Errorf("V3 feature order mismatch at %d", i)
		}
	}
	if len(m.Heads) != len(HeadOrderV3) || !m.DryRunRequired || m.PhysicalActionExecuted || m.RealVisionMetrics || m.Promoted {
		return fmt.Errorf("invalid V3 head or dry-run contract")
	}
	for i, head := range HeadOrderV3 {
		if m.Heads[i] != head || len(m.Labels[head]) != len(LabelsV3[head]) {
			return fmt.Errorf("invalid V3 head %s", head)
		}
		for j, label := range LabelsV3[head] {
			if m.Labels[head][j] != label {
				return fmt.Errorf("V3 label order mismatch for %s", head)
			}
		}
	}
	return nil
}

type v3CPUHead struct {
	Schema     string     `json:"schema"`
	Head       string     `json:"head"`
	InputSize  int        `json:"input_size"`
	OutputSize int        `json:"output_size"`
	Labels     []string   `json:"labels"`
	Layers     []cpuLayer `json:"layers"`
}

type CPUBundleMLPV3 struct {
	Manifest V3Manifest
	Backbone []cpuLayer
	Heads    map[string]v3CPUHead
}

func LoadCPUBundleV3(dir string) (*CPUBundleMLPV3, error) {
	body, err := os.ReadFile(filepath.Join(dir, "MANIFEST.v3.json"))
	if err != nil {
		return nil, err
	}
	var manifest V3Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	readArtifact := func(name string) ([]byte, error) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if expected := manifest.Hashes[name]; expected == "" || fmt.Sprintf("%x", sha256.Sum256(b)) != expected {
			return nil, fmt.Errorf("V3 artifact hash mismatch for %s", name)
		}
		return b, nil
	}
	readLayers := func(name string) ([]cpuLayer, error) {
		b, err := readArtifact(name)
		if err != nil {
			return nil, err
		}
		var value struct {
			Schema string     `json:"schema"`
			Layers []cpuLayer `json:"layers"`
		}
		if err := json.Unmarshal(b, &value); err != nil {
			return nil, err
		}
		if value.Schema != "synora.cognitive-cpu-mlp/v3-backbone" || len(value.Layers) == 0 {
			return nil, fmt.Errorf("invalid V3 backbone")
		}
		return value.Layers, nil
	}
	backbone, err := readLayers("backbone.cpu.json")
	if err != nil {
		return nil, err
	}
	if err := validateV3Layers(backbone, CognitiveVectorSizeV3, manifest.HiddenSize); err != nil {
		return nil, err
	}
	bundle := &CPUBundleMLPV3{Manifest: manifest, Backbone: backbone, Heads: make(map[string]v3CPUHead, len(HeadOrderV3))}
	for _, head := range HeadOrderV3 {
		name := manifest.Artifacts[head]["artifact"]
		if name == "" {
			name = head + ".cpu.json"
		}
		b, err := readArtifact(name)
		if err != nil {
			return nil, err
		}
		var model v3CPUHead
		if err := json.Unmarshal(b, &model); err != nil {
			return nil, err
		}
		if model.Schema != "synora.cognitive-cpu-mlp/v3-head" || model.Head != head || model.InputSize != manifest.HiddenSize || model.OutputSize != len(LabelsV3[head]) || len(model.Labels) != len(LabelsV3[head]) {
			return nil, fmt.Errorf("invalid V3 head contract for %s", head)
		}
		if err := validateV3Layers(model.Layers, manifest.HiddenSize, model.OutputSize); err != nil {
			return nil, err
		}
		bundle.Heads[head] = model
	}
	return bundle, nil
}

func validateV3Layers(layers []cpuLayer, input, output int) error {
	for _, layer := range layers {
		if layer.InputSize != input || layer.OutputSize <= 0 || len(layer.Weights) != layer.InputSize*layer.OutputSize || len(layer.Bias) != layer.OutputSize || (layer.Activation != "relu" && layer.Activation != "identity") {
			return fmt.Errorf("invalid V3 CPU layer")
		}
		input = layer.OutputSize
	}
	if input != output {
		return fmt.Errorf("V3 layer output mismatch")
	}
	return nil
}

func runCPULayersV3(layers []cpuLayer, input []float32) []float32 {
	current := append([]float32(nil), input...)
	for _, layer := range layers {
		next := make([]float32, layer.OutputSize)
		for o := range next {
			value := layer.Bias[o]
			for i, x := range current {
				value += layer.Weights[o*layer.InputSize+i] * x
			}
			if layer.Activation == "relu" && value < 0 {
				value = 0
			}
			next[o] = value
		}
		current = next
	}
	return current
}

func (m *CPUBundleMLPV3) RunV3(ctx context.Context, encoded EncodedSnapshotV3, snapshot CognitiveSnapshotV3) (MLPOutputV3, map[string]float64, error) {
	if m == nil {
		return MLPOutputV3{}, nil, fmt.Errorf("V3 CPU bundle unavailable")
	}
	if err := encoded.Validate(); err != nil {
		return MLPOutputV3{}, nil, err
	}
	if err := snapshot.Validate(); err != nil {
		return MLPOutputV3{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return MLPOutputV3{}, nil, err
	}
	started := time.Now()
	hidden := runCPULayersV3(m.Backbone, encoded.Values[:])
	logits := make(map[string][]float32, len(HeadOrderV3))
	latency := make(map[string]float64, len(HeadOrderV3))
	for _, head := range HeadOrderV3 {
		began := time.Now()
		logits[head] = runCPULayersV3(m.Heads[head].Layers, hidden)
		latency[head] = float64(time.Since(began).Microseconds()) / 1000
	}
	choose := func(head string) (string, float32) {
		probabilities := softmax(logits[head])
		index := argmax(probabilities)
		return LabelsV3[head][index], probabilities[index]
	}
	danger, dangerScore := choose("danger")
	incident, incidentScore := choose("incident")
	task, taskScore := choose("task")
	action, actionScore := choose("action")
	communication, communicationScore := choose("communication_intent")
	latency["shared_backbone"] = float64(time.Since(started).Microseconds()) / 1000
	return MLPOutputV3{DangerLabel: danger, DangerScore: dangerScore, Incident: incident, IncidentConfidence: incidentScore, Task: task, TaskConfidence: taskScore, Action: action, ActionConfidence: actionScore, Communication: CommunicationIntentV3{Intent: communication, TemplateID: TemplateIDV3(communication)}, CommunicationConfidence: communicationScore, ModelVersion: "cognitive-v3:" + m.Manifest.EncoderVersion}, latency, nil
}

var CommunicationTemplatesV3 = map[string]CommunicationTemplate{
	CommunicationNeutralPresenceNoticeV3: {CommunicationNeutralPresenceNoticeV3, "1.0.0", "Bonjour. Votre présence a été détectée."},
	CommunicationRequestIdentityV3:       {CommunicationRequestIdentityV3, "1.0.0", "Bonjour. Votre présence a été détectée. Si vous êtes autorisé, veuillez vous identifier."},
	CommunicationWellnessCheckV3:         {CommunicationWellnessCheckV3, "1.0.0", "Est-ce que tout va bien ? Une situation inhabituelle a été détectée."},
}

func TemplateIDV3(intent string) string {
	if item, ok := CommunicationTemplatesV3[intent]; ok {
		return item.ID + "@" + item.Version
	}
	return ""
}

func ApplyCommunicationGateV3(snapshot CognitiveSnapshotV3, output MLPOutputV3) CommunicationAssessmentV3 {
	assessment := CommunicationAssessmentV3{Intent: output.Communication, Capability: "announce", Status: "not_requested", PhysicalAudioPlayed: false}
	if output.Communication.Intent == CommunicationNoneV3 {
		assessment.Reasons = []string{"model_intent_none"}
		return assessment
	}
	if !snapshot.BaseV2.Communication.AnnounceAvailable || snapshot.BaseV2.Communication.TTSStatus != TTSAvailable {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"tts_capability_unavailable"}
		return assessment
	}
	if snapshot.BaseV2.Communication.CooldownActive {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"communication_cooldown"}
		return assessment
	}
	if snapshot.Vision.RiskStatus == RiskUncertain || snapshot.Vision.RiskStatus == RiskSuspected || snapshot.Vision.RiskStatus == RiskConfirmed {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"high_risk_communication_forbidden"}
		return assessment
	}
	if output.DangerLabel == "high" || output.DangerLabel == "critical" {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"danger_high_or_critical"}
		return assessment
	}
	if output.Incident == "interior_intrusion" || output.Incident == "perimeter_presence" {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"intrusion_probable"}
		return assessment
	}
	if snapshot.Vision.FaceStatus == FaceV3Unknown && (snapshot.Vision.RiskStatus == RiskSuspected || snapshot.Vision.RiskStatus == RiskConfirmed || output.DangerLabel == "high" || output.DangerLabel == "critical") {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"identity_unknown_high_risk"}
		return assessment
	}
	if snapshot.Vision.CameraUncertainty && (snapshot.Vision.RiskStatus == RiskSuspected || snapshot.Vision.RiskStatus == RiskConfirmed || output.DangerLabel == "high" || output.DangerLabel == "critical") {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"vision_state_uncertain_high_risk"}
		return assessment
	}
	if !snapshot.Vision.RiskQualitySufficient && snapshot.Vision.RiskStatus != RiskNotAvailable && snapshot.Vision.RiskStatus != RiskNotRequested && snapshot.Vision.RiskStatus != "" {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"risk_quality_insufficient"}
		return assessment
	}
	if snapshot.Vision.FallState == FallV3Candidate || snapshot.Vision.FallState == FallV3Confirmed || output.DangerLabel == "critical" {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"high_risk_communication_forbidden"}
		return assessment
	}
	template, ok := CommunicationTemplatesV3[output.Communication.Intent]
	if !ok {
		assessment.Status = "blocked"
		assessment.Reasons = []string{"template_unavailable"}
		return assessment
	}
	assessment.Status = "allowed_dry_run"
	assessment.RenderedText = template.Text
	assessment.Reasons = []string{"template_versioned", "discovery_announce_only", "active_dry_run"}
	return assessment
}

func ApplyActionGateV3(snapshot CognitiveSnapshotV3, output MLPOutputV3) ActionAssessment {
	action := ActionAssessment{Proposed: ActionIntent{Action: output.Action, Topology: snapshot.BaseV2.Topology, Capability: output.Action}, Status: "blocked", PhysicalActionExecuted: false}
	if output.Action == "no_action" {
		action.Status = "not_requested"
		action.Reasons = []string{"model_no_action"}
		return action
	}
	action.Status = "allowed_dry_run"
	action.Reasons = []string{"active_dry_run", "physical_execution_disabled"}
	return action
}

func BuildDecisionV3(snapshot CognitiveSnapshotV3, output MLPOutputV3, latencies map[string]float64, now time.Time) DecisionV3 {
	decision := DecisionV3{SchemaVersion: "cognitive-decision/v3", Status: "available", Mode: "active_dry_run", Source: "mlp-v3-candidate", HeadOrder: append([]string(nil), HeadOrderV3[:]...), InputDimension: CognitiveVectorSizeV3, DangerLabel: output.DangerLabel, DangerScore: output.DangerScore, Incident: output.Incident, Task: output.Task, HeadLatencyMS: latencies, PhysicalActionExecuted: false, GeneratedAt: now.UTC()}
	decision.Communication = ApplyCommunicationGateV3(snapshot, output)
	decision.Action = ApplyActionGateV3(snapshot, output)
	if output.Action == "announce" && decision.Communication.Status != "allowed_dry_run" {
		decision.Action.Status = "blocked"
		decision.Action.Reasons = append(decision.Action.Reasons, "communication_gate_blocked")
	}
	return decision
}
