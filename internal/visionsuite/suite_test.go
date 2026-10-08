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
	if len(states) != 5 {
		t.Fatalf("expected five declared inactive modules, got %d", len(states))
	}
	for _, state := range states {
		if state.State != "not_configured" {
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
	if len(digest) != 64 || registry.SchemaVersion != "synora.vision.module-registry/v1" || len(states) != 5 {
		t.Fatalf("invalid module registry: %+v", registry)
	}
	for name, state := range states {
		if state.State != "not_configured" || state.ModelVersion != "" || state.ModelSHA256 != "" {
			t.Fatalf("module %s has an implicit model configuration: %+v", name, state)
		}
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
	for _, forbidden := range []string{"clip_relative_path", "face_known/face_known_01.mp4", "resident_test_01", "Alice", "plate_text", "bbox", "embedding"} {
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
	slot := Slot{Suite: "face_known", CaseID: "face_known_01", ClipRelativePath: "face_known/slot.mp4", ClipSHA256: hex.EncodeToString(mediaDigest[:]), Module: ModuleFace, AssetStatus: "available", Expected: Expectation{State: "recognized", ConfidenceMinimum: intPointer(80)}}
	manifest := Manifest{SchemaVersion: ManifestSchema, Version: "unit", MediaRootEnv: "SYNORA_VISION_MEDIA_ROOT", Suites: []Slot{slot}}
	evidenceBytes, err := os.ReadFile("../../pkg/contract/testdata/v1/vision-evidence-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := contract.DecodeVisionEvidenceV1(evidenceBytes)
	if err != nil {
		t.Fatal(err)
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
	report := Execute(context.Background(), manifest, "digest", mediaRoot, map[string]string{ModuleFace: modelPath}, map[string]Module{ModuleFace: plugin}, pipeline)
	if !called || report.Executed != 1 || report.Qualified != 0 || !report.InferenceRun || report.Cases[0].Status != "executed" || report.Cases[0].SemanticResult != "recognized" || report.Cases[0].ConfidencePercent != 93 {
		t.Fatalf("expected a redacted, unqualified Evidence V1 execution: %+v", report)
	}
	if report.Cases[0].Pipeline.PhysicalAction || report.Cases[0].Pipeline.AudioRendered || report.Cases[0].Pipeline.NetworkAccess || report.Cases[0].Pipeline.RawVisionForwarded {
		t.Fatalf("unsafe pipeline flags were present: %+v", report.Cases[0].Pipeline)
	}
}

type resultPlugin struct {
	descriptor ModuleDescriptor
	evidence   contract.VisionEvidenceV1
}

func (p *resultPlugin) Descriptor() ModuleDescriptor { return p.descriptor }
func (p *resultPlugin) Run(_ context.Context, input ModuleInput) (ModuleResult, error) {
	if input.CaseID == "" || input.Suite == "" || input.Module != ModuleFace {
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
