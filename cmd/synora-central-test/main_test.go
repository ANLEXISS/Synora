package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"synora/pkg/contract"
)

func TestCentralFixtureManifestHasRequiredMinimum(t *testing.T) {
	manifest, err := loadManifest("../../testdata/central-e2e-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	available := len(manifest.Cases) + generatedScenarioCount(manifest.Generated)
	if manifest.MinimumCases < 80 || available < manifest.MinimumCases {
		t.Fatalf("fixture minimum is not met: %d/%d", available, manifest.MinimumCases)
	}
	if manifest.SchemaVersion != "synora.central-e2e-manifest/v1" || manifest.Seed == 0 || manifest.LogicalDate == "" {
		t.Fatalf("invalid manifest metadata: %+v", manifest)
	}
}

func TestCentralGeneratedSuitesAreFixedAndSeparated(t *testing.T) {
	manifest, err := loadManifest("../../testdata/central-e2e-v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"pose_movement": 90, "face_aggregate": 70, "communication_gate": 50, "camera_health": 45, "rtmpose_runtime": 40}
	if len(manifest.Generated) != len(want) {
		t.Fatalf("generated suite count=%d, want %d", len(manifest.Generated), len(want))
	}
	for _, suite := range manifest.Generated {
		if suite.Bundle != "v3" || suite.Seed == 0 || suite.Count != want[suite.Family] {
			t.Fatalf("unexpected generated suite: %+v", suite)
		}
		if suite.Family == "" || suite.IDPrefix == "" || suite.Suite == "" {
			t.Fatalf("generated suite is not declarative: %+v", suite)
		}
	}
	first := generatedFixture(manifest.Generated[0], 0)
	second := generatedFixture(manifest.Generated[0], 0)
	left, _ := json.Marshal(first)
	right, _ := json.Marshal(second)
	if string(left) != string(right) {
		t.Fatal("generated fixture expansion is not deterministic")
	}
	if containsForbiddenJSON(first.Messages[0].Payload) {
		t.Fatal("generated fixture contains a forbidden raw Vision field")
	}
}

func TestCentralSummaryRedactsPayload(t *testing.T) {
	payload := []byte(`{"media_ref":"must-not-appear","keypoints":[[1,2,0.9]],"status":"accepted"}`)
	digest := sha256.Sum256(payload)
	record := summarizeMessage(contract.Message{Type: "test", Source: "camera", Target: "core", Payload: payload})
	if record.Status != "accepted" {
		t.Fatalf("status was not retained: %+v", record)
	}
	if record.PayloadSHA != hex.EncodeToString(digest[:]) {
		t.Fatalf("payload digest mismatch: %+v", record)
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if containsForbiddenJSON(encoded) {
		t.Fatal("redacted trace unexpectedly contains a forbidden Vision field")
	}
}

func TestCentralForbiddenPayloadDetection(t *testing.T) {
	for _, payload := range []string{
		`{"frame":"raw"}`,
		`{"bbox":[1,2,3,4]}`,
		`{"raw_keypoints":[[1,2,0.8]]}`,
		`{"embedding":[0.1,0.2]}`,
		`{"local_track_id":"track-1"}`,
	} {
		if !containsForbiddenJSON([]byte(payload)) {
			t.Errorf("forbidden payload was not detected: %s", payload)
		}
	}
}

func TestMLPObservationUsesNamedProbabilitiesAndNoRawVision(t *testing.T) {
	model, err := loadTestCPUModel("../../build/cognitive-mlp-v3-candidate", true)
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([]float32, cognitiveVectorSizeForTestV3)
	probabilities := model.probabilities(encoded)
	for head, values := range probabilities {
		total := 0.0
		for _, value := range values {
			total += value
		}
		if total < 0.99999 || total > 1.00001 {
			t.Fatalf("probability sum for %s is %v", head, total)
		}
	}
	observation, err := json.Marshal(mlpObservation{Backend: "cpu-bundle-v3-candidate", Heads: map[string]headObservation{"danger": makeHeadObservationWithLabels("none", 1, []float64{1, 0, 0, 0, 0}, model.Labels["danger"])}})
	if err != nil {
		t.Fatal(err)
	}
	if containsForbiddenJSON(observation) {
		t.Fatal("MLP observation contains a raw Vision field")
	}
}

const cognitiveVectorSizeForTestV3 = 86
