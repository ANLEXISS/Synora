package cognitive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const RuntimeManifestSchema = "synora.cognitive-runtime-manifest/v1"

// RuntimeManifest is the signed-by-hash contract for the four CPU reference
// heads. It is deliberately separate from the model implementation: labels,
// thresholds and post-processing are policy data and must come from here.
type RuntimeManifest struct {
	Schema             string                 `json:"schema"`
	BundleSchema       string                 `json:"bundle_schema"`
	Encoder            RuntimeEncoder         `json:"encoder"`
	Heads              map[string]RuntimeHead `json:"heads"`
	DryRunRequired     bool                   `json:"dry_run_required"`
	SourceManifestHash string                 `json:"source_manifest_sha256"`
}

type RuntimeEncoder struct {
	Schema         string `json:"schema"`
	FeatureSize    int    `json:"feature_size"`
	Implementation string `json:"implementation"`
}

type RuntimeHead struct {
	Source           string      `json:"source"`
	SourceSHA256     string      `json:"source_sha256"`
	InputSize        int         `json:"input_size"`
	Labels           []string    `json:"labels"`
	Phases           []string    `json:"phases,omitempty"`
	Thresholds       []float32   `json:"thresholds,omitempty"`
	PolicyCost       [][]float32 `json:"policy_cost,omitempty"`
	PolicyCostWeight *float32    `json:"policy_cost_weight,omitempty"`
	ONNX             string      `json:"onnx"`
	CPU              string      `json:"cpu"`
	Postprocess      string      `json:"postprocess"`
}

type BundleVerification struct {
	ManifestSHA256 string
	Runtime        RuntimeManifest
	BundleDir      string
}

func LoadRuntimeManifest(path string) (RuntimeManifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return RuntimeManifest{}, fmt.Errorf("read runtime manifest: %w", err)
	}
	var manifest RuntimeManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return RuntimeManifest{}, fmt.Errorf("parse runtime manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return RuntimeManifest{}, err
	}
	return manifest, nil
}

func (m RuntimeManifest) Validate() error {
	if m.Schema != RuntimeManifestSchema || m.BundleSchema == "" {
		return fmt.Errorf("invalid cognitive runtime manifest schema")
	}
	if m.Encoder.Schema != StateEncoderSchemaVersion || m.Encoder.FeatureSize != EncoderV4Size || m.Encoder.Implementation != EncoderV4ID+"/"+EncoderV4Version {
		return fmt.Errorf("runtime manifest requires %s/%d (%s/%s)", StateEncoderSchemaVersion, EncoderV4Size, EncoderV4ID, EncoderV4Version)
	}
	if len(m.Heads) != 4 || !m.DryRunRequired {
		return fmt.Errorf("runtime manifest must declare danger, incident, task and action heads")
	}
	for _, name := range []string{DangerHead, IncidentHead, TaskHead, ActionHead} {
		head, ok := m.Heads[name]
		if !ok || strings.TrimSpace(head.Source) == "" || len(head.Labels) == 0 || head.InputSize <= 0 || len(head.SourceSHA256) != sha256.Size*2 {
			return fmt.Errorf("runtime manifest head %q is incomplete", name)
		}
		if filepath.Base(head.Source) != head.Source {
			return fmt.Errorf("runtime manifest head %q has unsafe source path", name)
		}
		if len(head.ONNX) == 0 || len(head.CPU) == 0 || len(head.Postprocess) == 0 {
			return fmt.Errorf("runtime manifest head %q is missing export contract", name)
		}
		wantPostprocess := map[string]string{DangerHead: "softmax", IncidentHead: "sigmoid-tags+softmax-phase", TaskHead: "sigmoid", ActionHead: "sigmoid+availability+ledger-mask"}[name]
		if head.Postprocess != wantPostprocess {
			return fmt.Errorf("runtime manifest head %q post-process %q is unsupported", name, head.Postprocess)
		}
	}
	if len(m.SourceManifestHash) != sha256.Size*2 {
		return fmt.Errorf("runtime manifest source manifest hash is invalid")
	}
	return nil
}

// VerifyBundle checks the immutable source manifest, the four .pt digests,
// dimensions and label/policy contracts before any model can be loaded.
func VerifyBundle(bundleDir, runtimeManifestPath string) (BundleVerification, error) {
	if strings.TrimSpace(bundleDir) == "" {
		return BundleVerification{}, errors.New("cognitive bundle directory is empty")
	}
	runtime, err := LoadRuntimeManifest(runtimeManifestPath)
	if err != nil {
		return BundleVerification{}, err
	}
	manifestPath := filepath.Join(bundleDir, "MANIFEST.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return BundleVerification{}, fmt.Errorf("read cognitive bundle manifest: %w", err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	manifestSHA := hex.EncodeToString(manifestHash[:])
	if !strings.EqualFold(manifestSHA, runtime.SourceManifestHash) {
		return BundleVerification{}, fmt.Errorf("cognitive bundle manifest hash mismatch: got %s want %s", manifestSHA, runtime.SourceManifestHash)
	}
	var source struct {
		BundleSchema string `json:"bundle_schema"`
		Artifacts    map[string]struct {
			SHA256   string `json:"sha256"`
			Bytes    int64  `json:"bytes"`
			Contract struct {
				FeatureSize      int         `json:"feature_size"`
				InputSize        int         `json:"input_size"`
				OutputSize       int         `json:"output_size"`
				Schema           string      `json:"schema"`
				VectorSchema     string      `json:"vector_schema"`
				Kinds            []string    `json:"kinds"`
				Phases           []string    `json:"phases"`
				Labels           []string    `json:"labels"`
				Tasks            []string    `json:"tasks"`
				Slots            []string    `json:"slots"`
				Thresholds       []float32   `json:"thresholds"`
				Threshold        []float32   `json:"threshold"`
				PolicyCost       [][]float32 `json:"policy_cost"`
				PolicyCostWeight *float32    `json:"policy_cost_weight"`
			} `json:"contract"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(manifestBytes, &source); err != nil {
		return BundleVerification{}, fmt.Errorf("parse cognitive bundle manifest: %w", err)
	}
	if source.BundleSchema != runtime.BundleSchema {
		return BundleVerification{}, fmt.Errorf("cognitive bundle schema mismatch: got %q want %q", source.BundleSchema, runtime.BundleSchema)
	}
	checksums := map[string]string{}
	sums, readErr := os.ReadFile(filepath.Join(bundleDir, "SHA256SUMS"))
	if readErr != nil {
		return BundleVerification{}, fmt.Errorf("read cognitive bundle checksums: %w", readErr)
	}
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			checksums[fields[1]] = strings.ToLower(fields[0])
		}
	}
	wantInputs := map[string]int{DangerHead: EncoderV4Size, IncidentHead: EncoderV4Size, TaskHead: 496, ActionHead: 531}
	for _, name := range []string{DangerHead, IncidentHead, TaskHead, ActionHead} {
		head := runtime.Heads[name]
		artifact, ok := source.Artifacts["artifacts/"+head.Source]
		if !ok {
			artifact, ok = source.Artifacts[head.Source]
		}
		if !ok {
			return BundleVerification{}, fmt.Errorf("bundle artifact for %s is missing", name)
		}
		if artifact.SHA256 == "" || !strings.EqualFold(artifact.SHA256, head.SourceSHA256) {
			return BundleVerification{}, fmt.Errorf("bundle manifest hash for %s does not match runtime manifest", name)
		}
		artifactPath := filepath.Join(bundleDir, "artifacts", head.Source)
		info, statErr := os.Stat(artifactPath)
		if statErr != nil || !info.Mode().IsRegular() {
			return BundleVerification{}, fmt.Errorf("bundle artifact %s is unavailable", head.Source)
		}
		if artifact.Bytes > 0 && info.Size() != artifact.Bytes {
			return BundleVerification{}, fmt.Errorf("bundle artifact %s size mismatch", head.Source)
		}
		digest, hashErr := fileSHA256(artifactPath)
		if hashErr != nil || !strings.EqualFold(digest, head.SourceSHA256) {
			return BundleVerification{}, fmt.Errorf("bundle artifact %s sha256 mismatch", head.Source)
		}
		sum, sumOK := checksums["artifacts/"+head.Source]
		if !sumOK || !strings.EqualFold(sum, head.SourceSHA256) {
			return BundleVerification{}, fmt.Errorf("SHA256SUMS entry for %s does not match", head.Source)
		}
		if head.InputSize != wantInputs[name] {
			return BundleVerification{}, fmt.Errorf("runtime head %s input size %d want %d", name, head.InputSize, wantInputs[name])
		}
		if err := verifyHeadContract(name, head, artifact.Contract); err != nil {
			return BundleVerification{}, err
		}
	}
	return BundleVerification{ManifestSHA256: manifestSHA, Runtime: runtime, BundleDir: bundleDir}, nil
}

func verifyHeadContract(name string, head RuntimeHead, contract struct {
	FeatureSize      int         `json:"feature_size"`
	InputSize        int         `json:"input_size"`
	OutputSize       int         `json:"output_size"`
	Schema           string      `json:"schema"`
	VectorSchema     string      `json:"vector_schema"`
	Kinds            []string    `json:"kinds"`
	Phases           []string    `json:"phases"`
	Labels           []string    `json:"labels"`
	Tasks            []string    `json:"tasks"`
	Slots            []string    `json:"slots"`
	Thresholds       []float32   `json:"thresholds"`
	Threshold        []float32   `json:"threshold"`
	PolicyCost       [][]float32 `json:"policy_cost"`
	PolicyCostWeight *float32    `json:"policy_cost_weight"`
}) error {
	wantSize := head.InputSize
	if contract.FeatureSize > 0 {
		wantSize = contract.FeatureSize
	}
	if contract.InputSize > 0 {
		wantSize = contract.InputSize
	}
	if wantSize != head.InputSize {
		return fmt.Errorf("bundle contract input size mismatch for %s", name)
	}
	labels := contract.Labels
	switch name {
	case DangerHead:
		labels = []string{"none", "low", "medium", "high", "critical"}
	case IncidentHead:
		labels = contract.Kinds
	case TaskHead:
		labels = contract.Tasks
	case ActionHead:
		labels = contract.Slots
	}
	if !sameStrings(labels, head.Labels) {
		return fmt.Errorf("bundle labels mismatch for %s", name)
	}
	if name == IncidentHead && !sameStrings(contract.Phases, head.Phases) {
		return fmt.Errorf("bundle phases mismatch for incident")
	}
	thresholds := contract.Thresholds
	if name == ActionHead {
		thresholds = contract.Threshold
	}
	if len(thresholds) > 0 && !sameFloat32(thresholds, head.Thresholds) {
		return fmt.Errorf("bundle thresholds mismatch for %s", name)
	}
	if name == DangerHead && (len(head.PolicyCost) != 5 || len(head.PolicyCost[0]) != 5 || head.PolicyCostWeight == nil) {
		return fmt.Errorf("danger policy cost is missing")
	}
	if name == DangerHead && !sameMatrix(contract.PolicyCost, head.PolicyCost) {
		return fmt.Errorf("danger policy cost mismatch")
	}
	if name == DangerHead && contract.PolicyCostWeight != nil && head.PolicyCostWeight != nil && *contract.PolicyCostWeight != *head.PolicyCostWeight {
		return fmt.Errorf("danger policy cost weight mismatch")
	}
	return nil
}

func VerifyExport(exportDir string, verification BundleVerification) error {
	runtimePath := filepath.Join(exportDir, "MANIFEST.runtime.json")
	exported, err := LoadRuntimeManifest(runtimePath)
	if err != nil {
		return err
	}
	if exported.SourceManifestHash != verification.ManifestSHA256 || exported.Encoder != verification.Runtime.Encoder {
		return errors.New("exported runtime manifest provenance mismatch")
	}
	for _, name := range []string{DangerHead, IncidentHead, TaskHead, ActionHead} {
		canonicalHead := verification.Runtime.Heads[name]
		head := exported.Heads[name]
		if head.Source != canonicalHead.Source || !strings.EqualFold(head.SourceSHA256, canonicalHead.SourceSHA256) || head.InputSize != canonicalHead.InputSize || !sameStrings(head.Labels, canonicalHead.Labels) || !sameStrings(head.Phases, canonicalHead.Phases) || !sameFloat32(head.Thresholds, canonicalHead.Thresholds) || head.Postprocess != canonicalHead.Postprocess {
			return fmt.Errorf("exported runtime head %s provenance mismatch", name)
		}
		path := filepath.Join(exportDir, head.CPU)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read exported %s CPU head: %w", name, readErr)
		}
		var cpu struct {
			Schema       string    `json:"schema"`
			Head         string    `json:"head"`
			InputSize    int       `json:"input_size"`
			Labels       []string  `json:"labels"`
			Thresholds   []float32 `json:"thresholds"`
			VectorSchema string    `json:"vector_schema"`
		}
		if err := json.Unmarshal(body, &cpu); err != nil || cpu.Schema != CPUMLPSchema || cpu.Head != name || cpu.InputSize != head.InputSize {
			return fmt.Errorf("exported %s CPU head contract is invalid", name)
		}
		labels := head.Labels
		if name == IncidentHead {
			labels = append(append([]string(nil), head.Labels...), head.Phases...)
		}
		if !sameStrings(cpu.Labels, labels) || !sameFloat32(cpu.Thresholds, head.Thresholds) || cpu.VectorSchema == "" {
			return fmt.Errorf("exported %s CPU head metadata is not manifest-derived", name)
		}
		if info, statErr := os.Stat(filepath.Join(exportDir, head.ONNX)); statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("exported %s ONNX artifact is unavailable", name)
		}
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sameFloat32(left, right []float32) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] && !(left[i] == 0 && right[i] == 0) {
			return false
		}
	}
	return true
}

func sameMatrix(left, right [][]float32) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !sameFloat32(left[i], right[i]) {
			return false
		}
	}
	return true
}

func (v BundleVerification) HeadNames() []string {
	names := make([]string, 0, len(v.Runtime.Heads))
	for name := range v.Runtime.Heads {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
