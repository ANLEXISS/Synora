package contract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func visionEvidenceFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/v1/vision-evidence-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestVisionEvidenceV1GoldenAndStrictRejection(t *testing.T) {
	body := visionEvidenceFixture(t)
	if _, err := DecodeVisionEvidenceV1(body); err != nil {
		t.Fatalf("golden fixture rejected: %v", err)
	}
	var base map[string]any
	if err := json.Unmarshal(body, &base); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(map[string]any){
		"unknown enum":                  func(v map[string]any) { v["topology"] = "lab" },
		"raw key":                       func(v map[string]any) { v["bbox"] = []int{1, 2, 3, 4} },
		"identifier":                    func(v map[string]any) { v["event_id"] = "camera-01" },
		"confidence out of range":       func(v map[string]any) { v["trigger"].(map[string]any)["confidence"] = 1.1 },
		"time mismatch":                 func(v map[string]any) { v["window_seconds"] = 8 },
		"simulated provenance mismatch": func(v map[string]any) { v["provenance"] = "simulated_test" },
		"empty evaluated support": func(v map[string]any) {
			v["activity"].(map[string]any)["support"].(map[string]any)["valid_evaluations"] = 0
		},
		"unsupported immobility": func(v map[string]any) { v["pose"].(map[string]any)["immobility_seconds"] = 2 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			copy := map[string]any{}
			encoded, _ := json.Marshal(base)
			_ = json.Unmarshal(encoded, &copy)
			mutate(copy)
			encoded, _ = json.Marshal(copy)
			if _, err := DecodeVisionEvidenceV1(encoded); err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}

	// Nested raw media/keypoint fields are rejected too, not merely stripped.
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	raw["pose"].(map[string]any)["keypoints"] = []int{1, 2}
	encoded, _ := json.Marshal(raw)
	if _, err := DecodeVisionEvidenceV1(encoded); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected strict nested rejection, got %v", err)
	}
}

func TestVisionEvidenceV1ClosedFunctionalVocabularies(t *testing.T) {
	body := visionEvidenceFixture(t)
	for family, states := range map[string][]string{
		"camera_health":    {"healthy", "degraded", "unavailable", "tamper_suspected"},
		"trigger":          {"motion", "human_probable", "vehicle_probable", "animal_probable", "tamper", "unknown"},
		"activity":         {"still", "normal", "rapid", "unknown"},
		"presence":         {"absent", "present", "ambiguous", "unknown"},
		"pose":             {"upright", "seated", "reclined", "ground", "ambiguous", "unknown"},
		"face":             {"recognized", "unknown", "ambiguous"},
		"plate":            {"recognized", "unknown", "ambiguous"},
		"sensitive_object": {"none", "generic", "unknown"},
		"media":            {"continuous", "gapped", "recovered", "unknown"},
	} {
		for _, state := range states {
			t.Run(family+"/"+state, func(t *testing.T) {
				var value map[string]any
				if err := json.Unmarshal(body, &value); err != nil {
					t.Fatal(err)
				}
				support := map[string]any{"valid_evaluations": float64(1), "continuity": "continuous", "supported_seconds": float64(0), "gap_count": float64(0)}
				switch family {
				case "camera_health", "trigger", "activity":
					value[family].(map[string]any)["state"] = state
					value[family].(map[string]any)["availability"] = "evaluated"
					value[family].(map[string]any)["confidence"] = .5
					value[family].(map[string]any)["quality"] = .5
					value[family].(map[string]any)["support"] = support
				case "presence":
					value["presence"].(map[string]any)["human"].(map[string]any)["state"] = state
					value["presence"].(map[string]any)["human"].(map[string]any)["availability"] = "evaluated"
					value["presence"].(map[string]any)["human"].(map[string]any)["confidence"] = .5
					value["presence"].(map[string]any)["human"].(map[string]any)["quality"] = .5
					value["presence"].(map[string]any)["human"].(map[string]any)["support"] = support
				case "pose":
					value["pose"].(map[string]any)["posture"] = state
					value["pose"].(map[string]any)["availability"] = "evaluated"
					value["pose"].(map[string]any)["posture_confidence"] = .5
					value["pose"].(map[string]any)["quality"] = .5
					value["pose"].(map[string]any)["support"] = support
				case "face", "plate":
					value[family].(map[string]any)["result"] = state
					value[family].(map[string]any)["availability"] = "evaluated"
					value[family].(map[string]any)["confidence"] = .5
					value[family].(map[string]any)["quality"] = .5
					value[family].(map[string]any)["support"] = support
				case "sensitive_object":
					value[family].(map[string]any)["category"] = state
					value[family].(map[string]any)["availability"] = "evaluated"
					value[family].(map[string]any)["confidence"] = .5
					value[family].(map[string]any)["quality"] = .5
					value[family].(map[string]any)["support"] = support
				case "media":
					value[family].(map[string]any)["episode_state"] = state
					value[family].(map[string]any)["availability"] = "evaluated"
					value[family].(map[string]any)["support"] = support
					if state == "gapped" || state == "recovered" {
						value[family].(map[string]any)["support"].(map[string]any)["gap_count"] = float64(1)
						value[family].(map[string]any)["support"].(map[string]any)["continuity"] = "gapped"
					}
				}
				encoded, _ := json.Marshal(value)
				if _, err := DecodeVisionEvidenceV1(encoded); err != nil {
					t.Fatalf("declared state rejected: %v", err)
				}
			})
		}
	}
}
