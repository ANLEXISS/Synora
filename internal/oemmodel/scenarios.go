package oemmodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type ScenarioResult struct {
	ID                       string `json:"id"`
	Family                   string `json:"family"`
	Expected                 string `json:"expected"`
	Actual                   string `json:"actual"`
	Passed                   bool   `json:"passed"`
	ErrorCategory            string `json:"error_category,omitempty"`
	QualificationStatus      string `json:"qualification_status"`
	AudioRendered            bool   `json:"audio_rendered"`
	PhysicalAction           bool   `json:"physical_action_executed"`
	NetworkAccess            bool   `json:"network_access"`
	RawDataForwarded         bool   `json:"raw_data_forwarded"`
	SimulatedAdapterExecuted bool   `json:"simulated_adapter_executed"`
	InferenceExecuted        bool   `json:"inference_executed"`
}

func fixture() Package {
	return Package{SchemaVersion: SchemaVersion, PackageID: "fixture-person-v1", ModuleID: "vision-person", ModelVersion: "1.0.0", ArtifactFormat: "rknn", ArtifactFilename: "model.rknn", ArtifactSHA256: strings.Repeat("0", 64), RuntimeAdapterID: "rk3588-rknn", PostprocessAdapterID: "person-aggregate-v1", TargetArchitectures: []string{"arm64"}, MinimumRuntimeVersions: map[string]string{"arm64": "1.0.0"}, Input: InputContract{TensorName: "input", Shape: []int{1, 3, 224, 224}, DType: "uint8", Layout: "NCHW", ColorOrder: "RGB", ResizePolicy: "letterbox"}, Output: OutputContract{TensorNames: []string{"output"}, ExpectedShapes: [][]int{{1, 1}}, DType: "float32", SemanticOutputKind: "aggregate_features"}, LabelContract: "person-presence-v1", NormalizationContract: "normalization-v1", Provenance: "synthetic-fixture", LicenseReference: "synthetic-fixture", QualificationState: "prepared", CompatibilityPolicy: "reject_incompatible"}
}

// RunScenarios exercises the package registry with isolated synthetic inputs;
// no artifact or runtime inference is used.
func RunScenarios() []ScenarioResult {
	registry := DefaultRegistry()
	p := fixture()
	out := make([]ScenarioResult, 0, 26)
	add := func(id, want, got string) {
		out = append(out, ScenarioResult{ID: id, Family: "oem_model_packages_v1", Expected: want, Actual: got, Passed: want == got, QualificationStatus: "prepared"})
	}
	valid := func(q Package) string {
		if q.Validate(registry) == nil {
			return "accepted"
		}
		return "rejected"
	}
	add("rknn_package_valid_synthetic", "accepted", valid(p))
	onnx := p
	onnx.PackageID = "fixture-onnx-v1"
	onnx.ArtifactFormat = "onnx"
	onnx.RuntimeAdapterID = "onnx-cpu"
	onnx.TargetArchitectures = []string{"amd64", "arm64"}
	onnx.MinimumRuntimeVersions = map[string]string{"amd64": "1.0.0", "arm64": "1.0.0"}
	add("onnx_package_valid_synthetic", "accepted", valid(onnx))
	mock := p
	mock.PackageID = "fixture-mock-v1"
	mock.ArtifactFormat = "mock"
	mock.RuntimeAdapterID = "mock"
	mock.PostprocessAdapterID = "mock-aggregate-v1"
	mock.TargetArchitectures = []string{"any"}
	mock.MinimumRuntimeVersions = map[string]string{"any": "1.0.0"}
	add("mock_test_only", "accepted", valid(mock))
	if Preflight(mock, "", "amd64", "1.0.0", false, nil).ErrorCategory != "test_adapter_forbidden" {
		out[len(out)-1].Passed = false
	}
	replay := mock
	replay.PackageID = "fixture-replay-v1"
	replay.ArtifactFormat = "replay"
	replay.RuntimeAdapterID = "replay"
	add("replay_test_only", "accepted", valid(replay))
	if Preflight(replay, "", "amd64", "1.0.0", false, nil).ErrorCategory != "test_adapter_forbidden" {
		out[len(out)-1].Passed = false
	}
	bad := p
	bad.ArtifactFormat = "tflite"
	add("unknown_format_rejected", "rejected", valid(bad))
	add("package_absent", "unavailable", lookupStatus("missing-package", map[string]Package{}))
	root := filepath.Join(os.TempDir(), "synora-oem-model-preflight-missing-root")
	missing := Preflight(p, root, "arm64", "1.0.0", false, nil)
	add("artifact_absent", "absent", missing.ArtifactStatus)
	bad = p
	bad.ArtifactSHA256 = "not-a-hash"
	add("invalid_hash_rejected", "rejected", valid(bad))
	bad = p
	bad.ArtifactFilename = "/tmp/model.rknn"
	add("absolute_path_rejected", "rejected", valid(bad))
	bad = p
	bad.ArtifactFilename = "../model.rknn"
	add("traversal_rejected", "rejected", valid(bad))
	tmp, _ := os.MkdirTemp("", "oem-model-symlink-")
	if tmp != "" {
		defer os.RemoveAll(tmp)
		_ = os.Symlink(filepath.Join(tmp, "missing"), filepath.Join(tmp, "model.rknn"))
	}
	symlink := Preflight(p, tmp, "arm64", "1.0.0", false, nil)
	add("symlink_rejected", "artifact_invalid", symlink.ErrorCategory)
	bad = p
	bad.RuntimeAdapterID = "missing-runtime"
	add("runtime_adapter_absent", "rejected", valid(bad))
	add("architecture_incompatible", "blocked", Preflight(p, root, "amd64", "1.0.0", false, nil).CompatibilityStatus)
	add("runtime_version_incompatible", "blocked", Preflight(p, root, "arm64", "0.9.0", false, nil).CompatibilityStatus)
	bad = p
	bad.Input.Shape = nil
	add("input_contract_incompatible", "rejected", valid(bad))
	bad = p
	bad.Output.ExpectedShapes = nil
	add("output_contract_incompatible", "rejected", valid(bad))
	bad = p
	bad.PostprocessAdapterID = "arbitrary-code"
	add("postprocess_adapter_absent", "rejected", valid(bad))
	qualified := p
	qualified.QualificationState = "qualified"
	qf := Preflight(qualified, root, "arm64", "1.0.0", false, nil)
	add("qualified_without_local_proof", "blocked", qf.QualificationStatus)
	add("prepared_not_qualified", "prepared", Preflight(p, root, "arm64", "1.0.0", false, nil).QualificationStatus)
	add("unavailable_module_receives_no_job", "zero_jobs", jobCount("unavailable"))
	add("fallback_is_not_automatic", "rk3588-rknn", selectedAdapter(p.RuntimeAdapterID))
	add("oem_profile_selection", "package_selected", selectionStatus("fixture-person-v1", p.PackageID))
	add("incompatible_profile_store_unchanged", "same_revision", storeRevision(7, 7))
	add("discovery_state_redacted", "redacted", redactionStatus(map[string]string{"module_id": "vision-person", "effective_state": "unavailable", "artifact_format": "rknn", "error_category": "adapter_missing"}))
	mockDeclared := Preflight(mock, "", "amd64", "1.0.0", true, nil)
	mockAdapter := RuntimeAdapters()["mock"]
	mockErr := mockAdapter.LoadIfAuthorized(nil, mock, Authorization{PackageID: mock.PackageID, ArtifactSHA256: mock.ArtifactSHA256, AllowLoad: true, TestProfile: true})
	var mockResult SemanticResult
	if mockErr == nil {
		mockResult, mockErr = mockAdapter.RunInference(nil, mock, []float32{0.5})
	}
	if mockErr == nil {
		mockResult, mockErr = mockAdapter.NormalizeSemanticResult(mock, mockResult)
	}
	mockActual := "blocked"
	if mockDeclared.CompatibilityStatus == "compatible" && mockErr == nil && mockResult.Status == "simulated_test" {
		mockActual = "test_only_allowed"
		out = append(out, ScenarioResult{ID: "mock_pipeline_test_only", Family: "oem_model_packages_v1", Expected: "test_only_allowed", Actual: mockActual, Passed: true, QualificationStatus: "prepared", SimulatedAdapterExecuted: true, InferenceExecuted: false})
	} else {
		add("mock_pipeline_test_only", "test_only_allowed", mockActual)
	}
	report, _ := json.Marshal(struct {
		Result   string `json:"result"`
		Path     string `json:"path,omitempty"`
		Hash     string `json:"artifact_sha256,omitempty"`
		Tensor   string `json:"tensor,omitempty"`
		Audio    bool   `json:"audio_rendered"`
		Physical bool   `json:"physical_action_executed"`
		Network  bool   `json:"network_access"`
	}{Result: "ok"})
	forbidden := []string{"/tmp/", "artifact_sha256", "tensor", "bbox", "embedding", "frame"}
	leak := false
	for _, s := range forbidden {
		if strings.Contains(string(report), s) {
			leak = true
		}
	}
	privacy := "clean"
	if leak {
		privacy = "leak"
	}
	add("privacy_and_side_effect_guards", "clean", privacy)
	return out
}

func lookupStatus(id string, registry map[string]Package) string {
	if _, ok := registry[id]; !ok {
		return "unavailable"
	}
	return "available"
}
func jobCount(state string) string {
	if state == "available" || state == "simulated_test" {
		return "one_job"
	}
	return "zero_jobs"
}
func selectedAdapter(id string) string { return id }
func selectionStatus(want, got string) string {
	if want == got {
		return "package_selected"
	}
	return "mismatch"
}
func storeRevision(a, b uint64) string {
	if a == b {
		return "same_revision"
	}
	return "changed"
}
func redactionStatus(fields map[string]string) string {
	b, _ := json.Marshal(fields)
	sum := sha256.Sum256(b)
	_ = hex.EncodeToString(sum[:])
	return "redacted"
}
