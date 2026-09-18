package cognitive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	CPUMLPSchema = "synora.cognitive-cpu-mlp/v1"
	DangerHead   = "danger"
	IncidentHead = "incident"
	TaskHead     = "task"
	ActionHead   = "action"
)

var ErrCPUModelUnavailable = errors.New("cognitive CPU model unavailable")

type cpuLinear struct {
	InputSize  int       `json:"input_size"`
	OutputSize int       `json:"output_size"`
	Activation string    `json:"activation"`
	Weights    []float32 `json:"weights"`
	Bias       []float32 `json:"bias"`
}

type cpuHeadFile struct {
	Schema       string               `json:"schema"`
	Head         string               `json:"head"`
	InputSize    int                  `json:"input_size"`
	OutputSize   int                  `json:"output_size"`
	Labels       []string             `json:"labels"`
	Thresholds   []float32            `json:"thresholds"`
	VectorSchema string               `json:"vector_schema"`
	OutputNames  []string             `json:"output_names"`
	Layers       []cpuLinear          `json:"layers"`
	SharedLayers []cpuLinear          `json:"shared_layers"`
	Heads        map[string]cpuLinear `json:"heads"`
}

type CPUMLPHead struct {
	Name             string
	InputSize        int
	OutputSize       int
	Labels           []string
	PhaseLabels      []string
	Thresholds       []float32
	VectorSchema     string
	Postprocess      string
	PolicyCost       [][]float32
	PolicyCostWeight *float32
	Layers           []cpuLinear
	SharedLayers     []cpuLinear
	Heads            map[string]cpuLinear
}

func loadCPUHead(path string) (CPUMLPHead, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return CPUMLPHead{}, fmt.Errorf("%w: %s: %v", ErrCPUModelUnavailable, path, err)
	}
	var file cpuHeadFile
	if err := json.Unmarshal(body, &file); err != nil {
		return CPUMLPHead{}, fmt.Errorf("%w: %s: %v", ErrCPUModelUnavailable, path, err)
	}
	if file.Schema != CPUMLPSchema || file.InputSize <= 0 || file.OutputSize <= 0 || len(file.Labels) != file.OutputSize {
		return CPUMLPHead{}, fmt.Errorf("%w: invalid %s manifest", ErrCPUModelUnavailable, path)
	}
	head := CPUMLPHead{Name: file.Head, InputSize: file.InputSize, OutputSize: file.OutputSize, Labels: append([]string(nil), file.Labels...), Thresholds: append([]float32(nil), file.Thresholds...), VectorSchema: file.VectorSchema, Layers: file.Layers, SharedLayers: file.SharedLayers, Heads: file.Heads}
	if len(head.Layers) == 0 && len(head.SharedLayers) == 0 {
		return CPUMLPHead{}, fmt.Errorf("%w: %s has no layers", ErrCPUModelUnavailable, path)
	}
	return head, nil
}

func (h CPUMLPHead) run(values []float32) ([]float32, error) {
	if len(values) != h.InputSize {
		return nil, fmt.Errorf("%s input has %d values, want %d", h.Name, len(values), h.InputSize)
	}
	return runLayers(values, h.Layers)
}

func (h CPUMLPHead) runIncident(values []float32) ([]float32, []float32, error) {
	shared, err := runLayers(values, h.SharedLayers)
	if err != nil {
		return nil, nil, err
	}
	tags, err := runLinear(shared, h.Heads["tag_logits"], false)
	if err != nil {
		return nil, nil, err
	}
	phases, err := runLinear(shared, h.Heads["phase_logits"], false)
	if err != nil {
		return nil, nil, err
	}
	return tags, phases, nil
}

func runLayers(values []float32, layers []cpuLinear) ([]float32, error) {
	current := append([]float32(nil), values...)
	for _, layer := range layers {
		var err error
		current, err = runLinear(current, layer, layer.Activation == "relu")
		if err != nil {
			return nil, err
		}
	}
	return current, nil
}

func runLinear(values []float32, layer cpuLinear, relu bool) ([]float32, error) {
	if len(values) != layer.InputSize || len(layer.Weights) != layer.InputSize*layer.OutputSize || len(layer.Bias) != layer.OutputSize {
		return nil, fmt.Errorf("invalid linear layer %dx%d for input %d", layer.InputSize, layer.OutputSize, len(values))
	}
	result := make([]float32, layer.OutputSize)
	for output := 0; output < layer.OutputSize; output++ {
		value := layer.Bias[output]
		for input := 0; input < layer.InputSize; input++ {
			value += values[input] * layer.Weights[output*layer.InputSize+input]
		}
		if relu && value < 0 {
			value = 0
		}
		result[output] = value
	}
	return result, nil
}

type CPUMLPBackend struct {
	Danger                CPUMLPHead
	Incident              CPUMLPHead
	Task                  CPUMLPHead
	Action                *CPUMLPHead
	RuntimeManifestSHA256 string
	latencyMu             sync.RWMutex
	lastHeadLatencyMS     map[string]float64
}

func NewCPUMLPBackend(modelDir string) (*CPUMLPBackend, error) {
	if strings.TrimSpace(modelDir) == "" {
		return nil, fmt.Errorf("%w: model directory is empty", ErrCPUModelUnavailable)
	}
	danger, err := loadCPUHead(filepath.Join(modelDir, "danger.cpu.json"))
	if err != nil {
		return nil, err
	}
	incident, err := loadCPUHead(filepath.Join(modelDir, "incident.cpu.json"))
	if err != nil {
		return nil, err
	}
	task, err := loadCPUHead(filepath.Join(modelDir, "task.cpu.json"))
	if err != nil {
		return nil, err
	}
	backend := &CPUMLPBackend{Danger: danger, Incident: incident, Task: task}
	if action, actionErr := loadCPUHead(filepath.Join(modelDir, "action.cpu.json")); actionErr == nil {
		backend.Action = &action
	}
	if danger.InputSize != EncoderV4Size || incident.InputSize != EncoderV4Size || task.InputSize != 496 || (backend.Action != nil && backend.Action.InputSize != 531) {
		return nil, fmt.Errorf("%w: manifest dimensions do not match v4 pipeline", ErrCPUModelUnavailable)
	}
	return backend, nil
}

// NewVerifiedCPUMLPBackend is the only constructor used by the E2E shadow
// path. The exported JSON weights provide arithmetic only; all dimensions,
// labels, thresholds and post-processing metadata are taken from the checked
// runtime manifest after the immutable source bundle has been verified.
func NewVerifiedCPUMLPBackend(modelDir string, verification BundleVerification) (*CPUMLPBackend, error) {
	if err := verification.Runtime.Validate(); err != nil {
		return nil, err
	}
	if err := VerifyExport(modelDir, verification); err != nil {
		return nil, err
	}
	backend, err := NewCPUMLPBackend(modelDir)
	if err != nil {
		return nil, err
	}
	applyRuntimeHead := func(head *CPUMLPHead, name string) error {
		manifestHead, ok := verification.Runtime.Heads[name]
		if !ok || head == nil {
			return fmt.Errorf("verified runtime head %q is missing", name)
		}
		if head.InputSize != manifestHead.InputSize {
			return fmt.Errorf("runtime head %q input size mismatch", name)
		}
		head.Labels = append([]string(nil), manifestHead.Labels...)
		head.Thresholds = append([]float32(nil), manifestHead.Thresholds...)
		head.Postprocess = manifestHead.Postprocess
		head.PolicyCost = append([][]float32(nil), manifestHead.PolicyCost...)
		head.PolicyCostWeight = manifestHead.PolicyCostWeight
		if name == IncidentHead {
			head.PhaseLabels = append([]string(nil), manifestHead.Phases...)
			head.OutputSize = len(manifestHead.Labels)
		}
		return nil
	}
	if err := applyRuntimeHead(&backend.Danger, DangerHead); err != nil {
		return nil, err
	}
	if err := applyRuntimeHead(&backend.Incident, IncidentHead); err != nil {
		return nil, err
	}
	if err := applyRuntimeHead(&backend.Task, TaskHead); err != nil {
		return nil, err
	}
	if backend.Action == nil {
		return nil, fmt.Errorf("verified action head is unavailable")
	}
	if err := applyRuntimeHead(backend.Action, ActionHead); err != nil {
		return nil, err
	}
	backend.RuntimeManifestSHA256 = verification.ManifestSHA256
	return backend, nil
}

func (b *CPUMLPBackend) ID() string { return "mlp-cpu-v1" }

func (b *CPUMLPBackend) HeadLatencyMS() map[string]float64 {
	if b == nil {
		return map[string]float64{}
	}
	b.latencyMu.RLock()
	defer b.latencyMu.RUnlock()
	result := make(map[string]float64, len(b.lastHeadLatencyMS))
	for name, value := range b.lastHeadLatencyMS {
		result[name] = value
	}
	return result
}

func (b *CPUMLPBackend) recordHeadLatency(values map[string]float64) {
	b.latencyMu.Lock()
	defer b.latencyMu.Unlock()
	b.lastHeadLatencyMS = make(map[string]float64, len(values))
	for name, value := range values {
		b.lastHeadLatencyMS[name] = value
	}
}

func (b *CPUMLPBackend) Run(ctx context.Context, input CognitiveInput, descriptor AdapterDescriptor) (CognitiveOutput, error) {
	if err := ctx.Err(); err != nil {
		return CognitiveOutput{}, err
	}
	if b == nil {
		return CognitiveOutput{}, ErrCPUModelUnavailable
	}
	headLatency := map[string]float64{}
	defer b.recordHeadLatency(headLatency)
	if input.EncodedState.SchemaVersion != StateEncoderSchemaVersion || len(input.EncodedState.Values) != EncoderV4Size {
		return CognitiveOutput{}, fmt.Errorf("CPU MLP requires %s/%d features", StateEncoderSchemaVersion, EncoderV4Size)
	}
	state := append([]float32(nil), input.EncodedState.Values...)
	dangerStarted := time.Now()
	dangerLogits, err := b.Danger.run(state)
	if err != nil {
		return CognitiveOutput{}, err
	}
	headLatency[DangerHead] = float64(time.Since(dangerStarted).Microseconds()) / 1000
	dangerProb := softmax(dangerLogits)
	masked := append([]float32(nil), state...)
	for i := 33; i < 37; i++ {
		masked[i] = 0
	}
	incidentStarted := time.Now()
	incidentTagsLogits, incidentPhaseLogits, err := b.Incident.runIncident(masked)
	if err != nil {
		return CognitiveOutput{}, err
	}
	headLatency[IncidentHead] = float64(time.Since(incidentStarted).Microseconds()) / 1000
	incidentTags := sigmoidAll(incidentTagsLogits)
	incidentPhase := softmax(incidentPhaseLogits)
	taskVector := append(append(append(append([]float32(nil), masked...), dangerProb...), incidentTags...), incidentPhase...)
	taskStarted := time.Now()
	taskLogits, err := b.Task.run(taskVector)
	if err != nil {
		return CognitiveOutput{}, err
	}
	headLatency[TaskHead] = float64(time.Since(taskStarted).Microseconds()) / 1000
	taskProb := sigmoidAll(taskLogits)

	phaseLabels := b.Incident.PhaseLabels
	if len(phaseLabels) == 0 {
		phaseCount := len(b.Incident.Heads["phase_logits"].Bias)
		if phaseCount <= len(b.Incident.Labels) {
			phaseLabels = b.Incident.Labels[len(b.Incident.Labels)-phaseCount:]
		}
	}
	generatedAt := input.CreatedAt.UTC()
	output := CognitiveOutput{SchemaVersion: SchemaVersion, RequestID: input.RequestID, TaskID: input.Task.ID, AdapterID: descriptor.ID, BackendID: b.ID(), AdvisoryOnly: true, Classification: labelAt(b.Danger.Labels, argmax(dangerProb)), InferredState: labelAt(phaseLabels, argmax(incidentPhase)), RequestedCapabilities: append([]string(nil), input.Task.RequestedCapabilities...), Confidence: float64(maxFloat(dangerProb)), DangerProbabilities: dangerProb, DangerLabel: labelAt(b.Danger.Labels, argmax(dangerProb)), GeneratedAt: generatedAt}
	output.DangerLogits = dangerContract(dangerLogits)
	output.IncidentTags = scoredLabels(b.Incident.Labels[:len(incidentTags)], incidentTags, b.Incident.Thresholds)
	output.IncidentPhase = labelAt(phaseLabels, argmax(incidentPhase))
	output.TaskScores = scoredLabels(b.Task.Labels, taskProb, b.Task.Thresholds)

	if b.Action != nil {
		actionStarted := time.Now()
		actionVector, availability, reasons := actionInput(state, dangerProb, input.ActionCatalog, input.ActionLedger, b.Action.Labels)
		actionLogits, actionErr := b.Action.run(actionVector)
		if actionErr != nil {
			return CognitiveOutput{}, actionErr
		}
		headLatency[ActionHead] = float64(time.Since(actionStarted).Microseconds()) / 1000
		actionProb := sigmoidAll(actionLogits)
		for i, slot := range b.Action.Labels {
			if i >= len(actionProb) || availability[i] == 0 {
				continue
			}
			output.ActionLogits = append(output.ActionLogits, ActionLogit{ActionID: slot, Logit: actionLogits[i], Probability: actionProb[i]})
			if actionProb[i] >= thresholdAt(b.Action.Thresholds, i, 0.5) {
				output.SelectedActionIDs = append(output.SelectedActionIDs, slot)
				output.ProposedActionIDs = append(output.ProposedActionIDs, slot)
			}
		}
		for i, slot := range b.Action.Labels {
			if i < len(actionProb) && actionProb[i] >= thresholdAt(b.Action.Thresholds, i, 0.5) && availability[i] == 0 {
				reason := reasons[slot]
				if reason == "" {
					reason = "device_store_unavailable"
				}
				output.FilteredActions = append(output.FilteredActions, FilteredAction{ID: slot, Reason: reason})
			}
		}
	}
	return output, nil
}

func actionInput(state, danger []float32, catalog ActionCatalog, ledger ActionLedgerSnapshot, labels []string) ([]float32, []float32, map[string]string) {
	availability := make([]float32, len(labels))
	reasons := make(map[string]string)
	for i, slot := range labels {
		if !catalog.Contains(slot) {
			reasons[slot] = "device_store_unavailable"
			continue
		}
		if status, ok := ledger.Completed(slot); ok {
			reasons[slot] = "ledger_" + status
			continue
		}
		availability[i] = 1
	}
	vector := append(append([]float32(nil), state...), danger...)
	vector = append(vector, availability...)
	ledgerFeatures := make([]float32, 1+2*len(labels))
	if ledger.IncidentOpen {
		ledgerFeatures[0] = 1
	}
	for _, entry := range ledger.RecentActions {
		for i, slot := range labels {
			if entry.Slot != slot {
				continue
			}
			if entry.Status == "executed" || entry.Status == "rejected" {
				ledgerFeatures[1+i] = 1
			}
			if entry.Status == "failed" {
				ledgerFeatures[1+len(labels)+i] = 1
			}
		}
	}
	return append(vector, ledgerFeatures...), availability, reasons
}

func scoredLabels(labels []string, probabilities, thresholds []float32) []ScoredLabel {
	result := make([]ScoredLabel, 0, len(probabilities))
	for i, probability := range probabilities {
		result = append(result, ScoredLabel{Label: labelAt(labels, i), Probability: probability, Threshold: thresholdAt(thresholds, i, 0.5), Selected: probability >= thresholdAt(thresholds, i, 0.5)})
	}
	return result
}
func dangerContract(values []float32) DangerLogits {
	result := DangerLogits{}
	if len(values) > 0 {
		result.None = values[0]
	}
	if len(values) > 1 {
		result.Low = values[1]
	}
	if len(values) > 2 {
		result.Medium = values[2]
	}
	if len(values) > 3 {
		result.High = values[3]
	}
	if len(values) > 4 {
		result.Critical = values[4]
	}
	return result
}
func sigmoidAll(values []float32) []float32 {
	result := make([]float32, len(values))
	for i, value := range values {
		result[i] = float32(1 / (1 + math.Exp(float64(-value))))
	}
	return result
}
func softmax(values []float32) []float32 {
	result := make([]float32, len(values))
	if len(values) == 0 {
		return result
	}
	maxValue := values[0]
	for _, value := range values[1:] {
		if value > maxValue {
			maxValue = value
		}
	}
	total := float64(0)
	for i, value := range values {
		result[i] = float32(math.Exp(float64(value - maxValue)))
		total += float64(result[i])
	}
	if total == 0 {
		return result
	}
	for i := range result {
		result[i] = float32(float64(result[i]) / total)
	}
	return result
}
func argmax(values []float32) int {
	best := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[best] {
			best = i
		}
	}
	return best
}
func maxFloat(values []float32) float32 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		if value > result {
			result = value
		}
	}
	return result
}
func labelAt(labels []string, index int) string {
	if index >= 0 && index < len(labels) {
		return labels[index]
	}
	return "unknown"
}
func thresholdAt(values []float32, index int, fallback float32) float32 {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return fallback
}
func nowUTC() (t time.Time) { return time.Now().UTC() }

var _ ModelBackend = (*CPUMLPBackend)(nil)
