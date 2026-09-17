package cognitive

import (
	"context"
	"os"
	"testing"
	"time"
)

func BenchmarkCPUMLPEndToEnd(b *testing.B) {
	modelDir := os.Getenv("SYNORA_COGNITIVE_MODEL_DIR")
	if modelDir == "" {
		b.Skip("set SYNORA_COGNITIVE_MODEL_DIR")
	}
	backend, err := NewCPUMLPBackend(modelDir)
	if err != nil {
		b.Fatal(err)
	}
	encoded, err := (V4StateEncoder{}).Encode(context.Background(), StateFrame{SchemaVersion: StateFrameSchemaVersion, CapturedAt: time.Unix(5, 0).UTC(), CurrentEvent: &StateEvent{Type: "motion.person.detected", Timestamp: time.Unix(4, 0).UTC()}})
	if err != nil {
		b.Fatal(err)
	}
	input := CognitiveInput{SchemaVersion: SchemaVersion, RequestID: "bench", Task: Task{ID: "bench", Kind: "sensor"}, EncodedState: encoded, ActionCatalog: ActionCatalog{SchemaVersion: ActionCatalogSchemaVersion, Actions: []ActionDescriptor{{ID: "observation.record@event_zone", Slot: "observation.record@event_zone", Capability: "observation.record", RiskClass: "low", Enabled: true}}}, ActionLedger: ActionLedgerSnapshot{SchemaVersion: ActionLedgerSchemaVersion}}
	descriptor := AdapterDescriptor{ID: "event_reasoning"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := backend.Run(context.Background(), input, descriptor); err != nil {
			b.Fatal(err)
		}
	}
}
