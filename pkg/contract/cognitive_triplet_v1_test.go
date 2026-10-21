package contract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCognitiveTripletGoldenContracts(t *testing.T) {
	root := "testdata/v1/"
	eventBody, err := os.ReadFile(root + "cognitive-event-frame-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	event, err := DecodeEventFrameV1(eventBody)
	if err != nil || event.SchemaVersion != EventFrameSchemaV1 {
		t.Fatalf("EventFrame golden rejected: %v", err)
	}
	contextBody, err := os.ReadFile(root + "cognitive-context-frame-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	context, err := DecodeContextFrameV1(contextBody)
	if err != nil || context.SchemaVersion != ContextFrameSchemaV1 {
		t.Fatalf("ContextFrame golden rejected: %v", err)
	}
	arbitrationBody, err := os.ReadFile(root + "cognitive-arbitration-frame-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	arbitration, err := DecodeArbitrationFrameV1(arbitrationBody)
	if err != nil || arbitration.SchemaVersion != ArbitrationFrameSchemaV1 {
		t.Fatalf("ArbitrationFrame golden rejected: %v", err)
	}
	bundleBody, err := os.ReadFile(root + "cognitive-triplet-bundle-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	_, err = DecodeTripletBundleV1(bundleBody, TripletContractHashes())
	if err != nil {
		t.Fatalf("triplet bundle golden rejected: %v", err)
	}
	degradedBody, err := os.ReadFile(root + "cognitive-event-frame-degraded-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	degraded, err := DecodeEventFrameV1(degradedBody)
	if err != nil || degraded.Envelope.Availability != "degraded" || degraded.Envelope.Provenance != "replay" {
		t.Fatalf("degraded/replay golden rejected: %v", err)
	}
	blockedBody, err := os.ReadFile(root + "cognitive-arbiter-blocked-output-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var blocked DecisionArbiterOutputV1
	if err := json.Unmarshal(blockedBody, &blocked); err != nil {
		t.Fatal(err)
	}
	if err := blocked.Validate(); err != nil {
		t.Fatalf("blocked arbiter output golden rejected: %v", err)
	}
}

func TestCognitiveTripletRejectsUnknownRawAndIncoherentValues(t *testing.T) {
	body, _ := os.ReadFile("testdata/v1/cognitive-event-frame-v1.json")
	cases := map[string]func(map[string]any){
		"unknown field":                             func(v map[string]any) { v["bbox"] = []int{1, 2, 3, 4} },
		"unknown provenance":                        func(v map[string]any) { v["envelope"].(map[string]any)["provenance"] = "camera_mock" },
		"confidence out of range":                   func(v map[string]any) { v["envelope"].(map[string]any)["quality"] = 1.1 },
		"negative evidence from unavailable vision": func(v map[string]any) { v["presence"].(map[string]any)["human"] = "absent" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(body, &value); err != nil {
				t.Fatal(err)
			}
			mutate(value)
			encoded, _ := json.Marshal(value)
			if _, err := DecodeEventFrameV1(encoded); err == nil {
				t.Fatal("invalid EventFrame accepted")
			}
		})
	}
	contextBody, _ := os.ReadFile("testdata/v1/cognitive-context-frame-v1.json")
	var contextValue map[string]any
	if err := json.Unmarshal(contextBody, &contextValue); err != nil {
		t.Fatal(err)
	}
	contextValue["local_path"] = "/secret"
	badContext, _ := json.Marshal(contextValue)
	if _, err := DecodeContextFrameV1(badContext); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("forbidden local path not rejected: %v", err)
	}
}

func TestTripletBundleRejectsMixedSchemasAndTaxonomy(t *testing.T) {
	hashes := TripletContractHashes()
	manifest := strings.Repeat("a", 64)
	bundle := TripletBundleV1{SchemaVersion: TripletBundleSchemaV1, EventModelManifestHash: manifest, ContextModelManifestHash: manifest, ArbiterModelManifestHash: manifest, EventFrameSchemaHash: hashes["event_frame_schema_hash"], ContextFrameSchemaHash: hashes["context_frame_schema_hash"], ArbitrationFrameSchemaHash: hashes["arbitration_frame_schema_hash"], LabelTaxonomyHash: hashes["label_taxonomy_hash"], CalibrationManifestHash: manifest, CompatibilityVersion: TripletCompatibilityV1}
	if err := bundle.Validate(hashes); err != nil {
		t.Fatal(err)
	}
	bundle.LabelTaxonomyHash = strings.Repeat("b", 64)
	if err := bundle.Validate(hashes); err == nil {
		t.Fatal("incompatible label taxonomy accepted")
	}
	bundle.LabelTaxonomyHash = hashes["label_taxonomy_hash"]
	bundle.CompatibilityVersion = "v2"
	if err := bundle.Validate(hashes); err == nil {
		t.Fatal("mixed compatibility version accepted")
	}
}

func TestTripletOutputsKeepAvailabilityAndEmbeddingMasksExplicit(t *testing.T) {
	if err := (EventCorrelationOutputV1{LocalImpact: "uncertain", EvidenceQuality: "unavailable", AvailabilityMask: "unavailable"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (EventCorrelationOutputV1{EventEmbedding: []float32{0}, LocalImpact: "uncertain", EvidenceQuality: "unavailable", AvailabilityMask: "unavailable"}).Validate(); err == nil {
		t.Fatal("unavailable branch accepted an ambiguous vector")
	}
	if err := (DecisionArbiterOutputV1{Danger: "unknown", Incident: "unknown", Task: "unknown", Action: "unknown", CommunicationIntent: "unknown", Availability: "unavailable"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
