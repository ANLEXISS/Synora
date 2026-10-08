package visionsuite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"synora/pkg/contract"
)

func TestCommittedVisionSuiteSlotsAreValidAndComplete(t *testing.T) {
	manifest, digest, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 || len(manifest.Suites) != 65 {
		t.Fatalf("unexpected manifest inventory size/hash: slots=%d hash=%q", len(manifest.Suites), digest)
	}
	want := map[string]int{"face_known": 10, "face_unknown": 10, "face_ambiguous": 5, "vehicle_presence": 10, "plate_reading": 10, "animal_presence": 10, "camera_health": 10}
	for suite, count := range List(manifest, digest).SuiteCounts {
		if want[suite] != count {
			t.Errorf("suite %s slots=%d want=%d", suite, count, want[suite])
		}
		delete(want, suite)
	}
	if len(want) != 0 {
		t.Fatalf("missing suite slots: %+v", want)
	}
	conditions := make(map[string]bool)
	for _, slot := range manifest.Suites {
		if slot.Suite == "face_ambiguous" {
			for _, tag := range slot.ConditionTags {
				conditions[tag] = true
			}
		}
	}
	for _, tag := range []string{"degraded", "profile", "occluded", "distant", "low_light", "identity_ambiguous"} {
		if !conditions[tag] {
			t.Errorf("face_ambiguous slots do not cover condition %q", tag)
		}
	}
}

func TestConditionTagsAreClosedRequiredAndSuiteRelevant(t *testing.T) {
	manifest, _, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"missing", func(m *Manifest) { m.Suites[0].ConditionTags = nil }},
		{"unknown", func(m *Manifest) { m.Suites[0].ConditionTags = []string{"resident_known", "invented_condition"} }},
		{"irrelevant", func(m *Manifest) { m.Suites[0].ConditionTags = []string{"identity_unknown"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := manifest
			candidate.Suites = append([]Slot(nil), manifest.Suites...)
			tc.mutate(&candidate)
			if ValidateManifest(candidate) == nil {
				t.Fatal("invalid condition tags were accepted")
			}
		})
	}
}

func TestInactiveModulesAreExplicitAndPlaceholdersNeverInvokePlugins(t *testing.T) {
	manifest, digest, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	states := InactiveModules()
	if len(states) != 6 {
		t.Fatalf("expected six declared modules, got %d", len(states))
	}
	for name, state := range states {
		if name == ModulePose {
			if state.State != "unavailable" {
				t.Fatalf("pose backend must remain explicitly unavailable without a model: %+v", state)
			}
		} else if state.State != "not_configured" {
			t.Fatalf("module was implicitly activated: %+v", state)
		}
	}
	plugin := &countingPlugin{}
	report := Execute(context.Background(), manifest, digest, t.TempDir(), map[string]string{ModuleFace: "/not/loaded/model"}, map[string]Module{ModuleFace: plugin}, nil)
	if plugin.calls != 0 || report.InferenceRun || report.MediaAbsent != 65 || report.ModelAbsent != 65 || report.Executed != 0 || report.Qualified != 0 {
		t.Fatalf("placeholder triggered inference or success: plugin_calls=%d report=%+v", plugin.calls, report)
	}
	for _, item := range report.Cases {
		if item.Status == "executed" || item.Status == "qualified" || item.InferenceExecuted {
			t.Fatalf("placeholder counted as model result: %+v", item)
		}
	}
}

func TestCommittedModuleRegistryIsStrictAndInactive(t *testing.T) {
	registry, states, digest, err := LoadRegistry("../../testdata/vision-v1/modules.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 || registry.SchemaVersion != "synora.vision.module-registry/v1" || len(states) != 6 {
		t.Fatalf("invalid module registry: %+v", registry)
	}
	for name, state := range states {
		want := "not_configured"
		if name == ModulePose {
			want = "unavailable"
		}
		if state.State != want || state.ModelVersion != "" || state.ModelSHA256 != "" {
			t.Fatalf("module %s has an implicit model configuration: %+v", name, state)
		}
	}
	if len(registry.Modules) != 6 || registry.Modules[0].OutputContract != contract.EventVisionEvidenceV1 {
		t.Fatalf("canonical module registry lacks the Evidence V1 output contract: %+v", registry)
	}
	manifest, manifestDigest, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateManifestPin(manifest, manifestDigest, registry); err != nil {
		t.Fatalf("committed suite manifest did not match its pin: %v", err)
	}
	registry.Modules[0].ManifestSHA256 = strings.Repeat("0", 64)
	if err := ValidateManifestPin(manifest, manifestDigest, registry); err == nil {
		t.Fatal("altered manifest pin was accepted")
	}
}

func TestFilterCaseRunsOnlyDeclaredCase(t *testing.T) {
	manifest, _, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := FilterCase(manifest, "face_known_03")
	if err != nil || len(filtered.Suites) != 1 || filtered.Suites[0].CaseID != "face_known_03" {
		t.Fatalf("case filter did not select the declared case: len=%d err=%v", len(filtered.Suites), err)
	}
	if _, err := FilterCase(manifest, "face_known_missing"); err == nil {
		t.Fatal("case filter accepted an undeclared case")
	}
}

func TestFaceLowQualityRemainsDistinctFromEvaluatedUnknown(t *testing.T) {
	lowQuality := contract.VisionEvidenceV1{
		Face:             contract.VisionSemanticResultV1{Availability: contract.VisionUnavailable, Result: "unknown"},
		RuntimeAggregate: &contract.VisionRuntimeAggregateV1{FaceStatus: "low_quality"},
	}
	unknown := contract.VisionEvidenceV1{
		Face: contract.VisionSemanticResultV1{Availability: contract.VisionEvaluated, Result: "unknown"},
	}
	if got := semanticState(lowQuality, ModuleFace); got != "low_quality" {
		t.Fatalf("insufficient face quality collapsed into %q", got)
	}
	if got := semanticState(unknown, ModuleFace); got != "unknown" {
		t.Fatalf("evaluated unknown face changed to %q", got)
	}
	if !matchesExpected("ambiguous_or_unavailable", semanticState(lowQuality, ModuleFace)) {
		t.Fatal("ambiguous/degraded face fixture did not accept an explicit low-quality outcome")
	}
}

func TestAvailableMediaRequiresVerifiedTechnicalMetadata(t *testing.T) {
	manifest, _, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	manifest.Suites[0].AssetStatus = "available"
	manifest.Suites[0].ClipSHA256 = strings.Repeat("a", 64)
	manifest.Suites[0].Technical = TechnicalMetadata{Container: "mp4", Codec: "h264", Width: 640, Height: 480, Frames: 30, Duration: "1s"}
	if err := ValidateManifest(manifest); err != nil {
		t.Fatalf("complete technical metadata rejected: %v", err)
	}
	manifest.Suites[0].Technical.Duration = "pending"
	if err := ValidateManifest(manifest); err == nil {
		t.Fatal("available media with unparseable duration was accepted")
	}
}

func TestUnconfiguredModuleNeverInvokesPluginEvenWithValidMedia(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "face_known"), 0700); err != nil {
		t.Fatal(err)
	}
	mediaBytes := []byte("synthetic unit bytes; not a video")
	mediaPath := filepath.Join(root, "face_known", "slot.mp4")
	if err := os.WriteFile(mediaPath, mediaBytes, 0600); err != nil {
		t.Fatal(err)
	}
	mediaHash := sha256.Sum256(mediaBytes)
	slot := Slot{Suite: "face_known", CaseID: "face_known_01", ClipRelativePath: "face_known/slot.mp4", ClipSHA256: hex.EncodeToString(mediaHash[:]), Module: ModuleFace, AssetStatus: "available", Technical: TechnicalMetadata{Container: "mp4", Codec: "h264", Width: 640, Height: 480, Frames: 1, Duration: "1s"}, Expected: Expectation{State: "recognized"}}
	manifest := Manifest{SchemaVersion: ManifestSchema, Version: "test", MediaRootEnv: "SYNORA_VISION_MEDIA_ROOT", Suites: []Slot{slot}}
	probeCalls := 0
	probe := func(context.Context, string) (MediaProbe, error) {
		probeCalls++
		return MediaProbe{Container: "mp4", Codec: "h264", Width: 640, Height: 480, Frames: 1, Duration: 1}, nil
	}
	plugin := &countingPlugin{}
	states := InactiveModules()
	report := ExecuteWithStatesAndProbe(context.Background(), manifest, "test-digest", root, states, map[string]string{ModuleFace: "/no/model"}, map[string]Module{ModuleFace: plugin}, nil, probe)
	if plugin.calls != 0 || report.InferenceRun || report.Executed != 0 || report.Qualified != 0 || probeCalls != 1 || report.Cases[0].Status != "model_absent" {
		t.Fatalf("unconfigured module was not fail-closed: plugin=%d probe=%d report=%+v", plugin.calls, probeCalls, report)
	}
	probeCalls = 0
	manifest.Suites[0].ClipSHA256 = strings.Repeat("b", 64)
	blocked := ExecuteWithStatesAndProbe(context.Background(), manifest, "test-digest", root, states, map[string]string{ModuleFace: "/no/model"}, map[string]Module{ModuleFace: plugin}, nil, probe)
	if plugin.calls != 0 || probeCalls != 0 || blocked.Cases[0].Status != "media_quarantined" || blocked.Executed != 0 {
		t.Fatalf("media without a matching declared hash was not rejected: plugin=%d probe=%d report=%+v", plugin.calls, probeCalls, blocked)
	}
	probeCalls = 0
	unsupported := ExecuteWithModeAndProbe(context.Background(), manifest, "test-digest", root, states, nil, nil, map[string]Module{ModuleFace: plugin}, nil, "real", probe)
	if plugin.calls != 0 || probeCalls != 0 || unsupported.ExecutionMode != "blocked" || unsupported.Cases[0].Reason != "execution_mode_not_supported" {
		t.Fatalf("live real mode was accepted by the offline suite runner: %+v", unsupported)
	}
}

func TestManifestRejectsTraversalURLsUnknownFieldsAndNonOpaqueSubjectRefs(t *testing.T) {
	manifest, _, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Manifest){
		func(m *Manifest) { m.Suites[0].ClipRelativePath = "../../private/video.mp4" },
		func(m *Manifest) { m.Suites[0].ClipRelativePath = "https://example.invalid/video.mp4" },
		func(m *Manifest) { m.Suites[0].Expected.SubjectRef = "Alice Example" },
		func(m *Manifest) { m.Suites[0].Expected.SubjectRef = "resident_test_999999" },
	}
	for index, mutate := range mutations {
		candidate := manifest
		candidate.Suites = append([]Slot(nil), manifest.Suites...)
		mutate(&candidate)
		if err := ValidateManifest(candidate); err == nil {
			t.Errorf("unsafe manifest mutation %d was accepted", index)
		}
	}
	body, _ := os.ReadFile("../../testdata/vision-v1/suites/manifest.json")
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	for _, key := range []string{"identity", "plate_text", "bbox", "crop", "keypoint", "embedding", "image", "frame", "local_track_id"} {
		candidate := jsonUnmarshalClone(t, raw)
		first := candidate["slots"].([]any)[0].(map[string]any)
		first["expected"].(map[string]any)[key] = "forbidden"
		encoded, _ := json.Marshal(candidate)
		path := filepath.Join(t.TempDir(), "unsafe.json")
		if err := os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadManifest(path); err == nil {
			t.Errorf("raw Vision field %q was not rejected", key)
		}
	}
}

func jsonUnmarshalClone(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(body, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func TestReportsNeverExposeMediaPathsOrOpaqueSubjectReferences(t *testing.T) {
	manifest, digest, err := LoadManifest("../../testdata/vision-v1/suites/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	report := Inspect(manifest, digest, "verify", t.TempDir(), InactiveModules())
	body, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"clip_relative_path", "face_known/face_known_01.mp4", "resident_test_01", "Alice", "plate_text", "bbox", "crop", "keypoint", "embedding", "identity", "local_track_id", "https://", "raw_media_blob"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("suite report leaked %q", forbidden)
		}
	}
}

func TestMediaPathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.mp4")
	if err := os.WriteFile(outside, []byte("placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "face_known"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "face_known", "slot.mp4")); err != nil {
		t.Fatal(err)
	}
	if _, err := containedFile(root, "face_known/slot.mp4"); err == nil {
		t.Fatal("symlink escape was accepted")
	}
}

func TestPluginResultMustBeEvidenceV1AndPipelineMustRemainDryRun(t *testing.T) {
	mediaRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(mediaRoot, "face_known"), 0700); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(mediaRoot, "face_known", "slot.mp4")
	if err := os.WriteFile(mediaPath, []byte("non-media unit fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(t.TempDir(), "model.placeholder")
	modelBytes := []byte("not a model; plugin contract test only")
	if err := os.WriteFile(modelPath, modelBytes, 0600); err != nil {
		t.Fatal(err)
	}
	modelDigest := sha256.Sum256(modelBytes)
	modelHash := hex.EncodeToString(modelDigest[:])
	mediaDigest := sha256.Sum256([]byte("non-media unit fixture"))
	slot := Slot{Suite: "face_known", CaseID: "face_known_01", ClipRelativePath: "face_known/slot.mp4", ClipSHA256: hex.EncodeToString(mediaDigest[:]), Module: ModuleFace, AssetStatus: "available", Technical: TechnicalMetadata{Container: "mp4", Codec: "h264", Width: 640, Height: 480, Frames: 30, Duration: "1s"}, Expected: Expectation{State: "recognized", ConfidenceMinimum: intPointer(80)}}
	manifest := Manifest{SchemaVersion: ManifestSchema, Version: "unit", MediaRootEnv: "SYNORA_VISION_MEDIA_ROOT", Suites: []Slot{slot}}
	evidenceBytes, err := os.ReadFile("../../pkg/contract/testdata/v1/vision-evidence-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := contract.DecodeVisionEvidenceV1(evidenceBytes)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Provenance = "simulated_test"
	evidence.SimulatedCamera = true
	if evidence.RuntimeAggregate != nil {
		evidence.RuntimeAggregate.RealDetection = false
		evidence.RuntimeAggregate.ReplaySimulation = true
	}
	evidence.Face.Availability = contract.VisionEvaluated
	evidence.Face.Result = "recognized"
	evidence.Face.Confidence = .93
	evidence.Face.Quality = .91
	evidence.Face.Support = contract.VisionSupportV1{ValidEvaluations: 2, Continuity: "continuous", SupportedSeconds: 1}
	plugin := &resultPlugin{descriptor: ModuleDescriptor{Name: ModuleFace, State: "available", ModelVersion: "test-plugin-v1", ModelSHA256: modelHash, InputCompatible: true, OutputCompatible: true, Reason: "ready"}, evidence: evidence}
	called := false
	pipeline := func(_ context.Context, actual contract.VisionEvidenceV1) (PipelineResult, error) {
		called = true
		if actual.SchemaVersion != contract.EventVisionEvidenceV1 {
			t.Fatal("pipeline received non-V1 evidence")
		}
		return PipelineResult{CoreReached: true, StoreWritten: true, SnapshotEncoded: true, MLPExecuted: true, SafetyGateChecked: true, DryRunResult: true}, nil
	}
	states := InactiveModules()
	states[ModuleFace] = ModuleDescriptor{Name: ModuleFace, State: "available", ModelVersion: "test-plugin-v1", ModelSHA256: modelHash, InputCompatible: true, OutputCompatible: true, Reason: "unit_fixture"}
	galleryPath := t.TempDir()
	probe := func(context.Context, string) (MediaProbe, error) {
		return MediaProbe{Container: "mp4", Codec: "h264", Width: 640, Height: 480, Frames: 30, Duration: 1}, nil
	}
	inputs := map[string]string{ModuleFace: galleryPath}
	report := ExecuteWithModeAndProbe(context.Background(), manifest, "digest", mediaRoot, states, map[string]string{ModuleFace: modelPath}, inputs, map[string]Module{ModuleFace: plugin}, pipeline, ExecutionSimulatedTest, probe)
	if !called || report.Executed != 1 || report.Qualified != 0 || !report.InferenceRun || report.Cases[0].Status != "executed" || report.Cases[0].SemanticResult != "recognized" || report.Cases[0].ConfidencePercent != 93 {
		t.Fatalf("expected a redacted, unqualified Evidence V1 execution: %+v", report)
	}
	if plugin.calls != 1 {
		t.Fatalf("expected exactly one synthetic plugin invocation, got %d", plugin.calls)
	}
	if plugin.galleryPath != galleryPath {
		t.Fatalf("consented external gallery path was not passed to face plugin: %q", plugin.galleryPath)
	}
	called = false
	noGallery := ExecuteWithStatesAndProbe(context.Background(), manifest, "digest", mediaRoot, states, map[string]string{ModuleFace: modelPath}, map[string]Module{ModuleFace: plugin}, pipeline, probe)
	if called || plugin.calls != 1 || noGallery.Cases[0].Status != "model_unavailable" || noGallery.InferenceRun {
		t.Fatalf("face module ran without a configured external gallery: calls=%d report=%+v", plugin.calls, noGallery)
	}
	if report.Cases[0].Pipeline.PhysicalAction || report.Cases[0].Pipeline.AudioRendered || report.Cases[0].Pipeline.NetworkAccess || report.Cases[0].Pipeline.RawVisionForwarded {
		t.Fatalf("unsafe pipeline flags were present: %+v", report.Cases[0].Pipeline)
	}
	reportJSON, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{mediaPath, modelPath, galleryPath, "MediaPath", "ModelPath", "GalleryPath", "clip_relative_path"} {
		if strings.Contains(string(reportJSON), forbidden) {
			t.Fatalf("execution report leaked a sensitive path/field %q", forbidden)
		}
	}
	badModelPath := filepath.Join(t.TempDir(), "different-model.placeholder")
	if err := os.WriteFile(badModelPath, []byte("different synthetic model bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	called = false
	badModelReport := ExecuteWithInputsAndProbe(context.Background(), manifest, "digest", mediaRoot, states, map[string]string{ModuleFace: badModelPath}, inputs, map[string]Module{ModuleFace: plugin}, pipeline, probe)
	if called || plugin.calls != 1 || badModelReport.Cases[0].Status != "model_unavailable" || badModelReport.InferenceRun {
		t.Fatalf("model hash mismatch was not rejected before plugin execution: calls=%d report=%+v", plugin.calls, badModelReport)
	}
	unsafePipelineCases := []struct {
		name   string
		mutate func(*PipelineResult)
	}{
		{"safety_gate_skipped", func(result *PipelineResult) { result.SafetyGateChecked = false }},
		{"physical_action", func(result *PipelineResult) { result.PhysicalAction = true }},
		{"audio_rendered", func(result *PipelineResult) { result.AudioRendered = true }},
		{"external_network", func(result *PipelineResult) { result.NetworkAccess = true }},
		{"raw_vision_forwarded", func(result *PipelineResult) { result.RawVisionForwarded = true }},
	}
	for _, testCase := range unsafePipelineCases {
		t.Run(testCase.name, func(t *testing.T) {
			pipelineCalled := false
			unsafePipeline := func(context.Context, contract.VisionEvidenceV1) (PipelineResult, error) {
				pipelineCalled = true
				result := PipelineResult{CoreReached: true, StoreWritten: true, SnapshotEncoded: true, MLPExecuted: true, SafetyGateChecked: true, DryRunResult: true}
				testCase.mutate(&result)
				return result, nil
			}
			unsafeReport := ExecuteWithModeAndProbe(context.Background(), manifest, "digest", mediaRoot, states, map[string]string{ModuleFace: modelPath}, inputs, map[string]Module{ModuleFace: plugin}, unsafePipeline, ExecutionSimulatedTest, probe)
			if !pipelineCalled || unsafeReport.Cases[0].Status != "failed" || unsafeReport.Qualified != 0 {
				t.Fatalf("unsafe pipeline result was accepted: %+v", unsafeReport.Cases[0])
			}
		})
	}
	evidence.Provenance, evidence.SimulatedCamera = "real", false
	if evidence.RuntimeAggregate != nil {
		evidence.RuntimeAggregate.RealDetection = true
		evidence.RuntimeAggregate.ReplaySimulation = false
	}
	plugin.evidence = evidence
	called = false
	provenanceMismatch := ExecuteWithModeAndProbe(context.Background(), manifest, "digest", mediaRoot, states, map[string]string{ModuleFace: modelPath}, inputs, map[string]Module{ModuleFace: plugin}, pipeline, ExecutionSimulatedTest, probe)
	if called || provenanceMismatch.Cases[0].Status != "failed" || provenanceMismatch.Cases[0].Reason != "evidence_provenance_does_not_match_execution_mode" {
		t.Fatalf("real provenance was confused with simulated_test mode: %+v", provenanceMismatch.Cases[0])
	}
}

type resultPlugin struct {
	descriptor  ModuleDescriptor
	evidence    contract.VisionEvidenceV1
	calls       int
	galleryPath string
}

func (p *resultPlugin) Descriptor() ModuleDescriptor { return p.descriptor }
func (p *resultPlugin) Run(_ context.Context, input ModuleInput) (ModuleResult, error) {
	p.calls++
	p.galleryPath = input.GalleryPath
	if input.CaseID == "" || input.Suite == "" || input.Module != ModuleFace || input.ExecutionMode != ExecutionSimulatedTest {
		return ModuleResult{}, context.Canceled
	}
	return ModuleResult{Evidence: &p.evidence, InferenceExecuted: true}, nil
}

func intPointer(value int) *int { return &value }

type countingPlugin struct{ calls int }

func (p *countingPlugin) Descriptor() ModuleDescriptor {
	return ModuleDescriptor{Name: ModuleFace, State: "available", InputCompatible: true, OutputCompatible: true}
}

func (p *countingPlugin) Run(context.Context, ModuleInput) (ModuleResult, error) {
	p.calls++
	return ModuleResult{Evidence: &contract.VisionEvidenceV1{}, InferenceExecuted: true}, nil
}
