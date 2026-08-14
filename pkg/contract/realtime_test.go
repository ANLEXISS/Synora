package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRealtimeEnvelopeValidatesVersionAndPayload(t *testing.T) {
	envelope := RealtimeEnvelope{
		SchemaVersion: RealtimeSchemaVersion,
		Type:          RealtimeIncidentUpdated,
		MessageID:     "ws-1",
		OccurredAt:    time.Date(2026, 8, 2, 17, 0, 0, 0, time.UTC),
		Source:        "core",
		Epoch:         "api-ws-1",
		Sequence:      7,
		Revision:      3,
		Payload:       json.RawMessage(`{"incident_id":"incident-1","reason":"viewed"}`),
	}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RealtimeEnvelope
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Sequence != envelope.Sequence || decoded.Revision != envelope.Revision || decoded.Type != envelope.Type {
		t.Fatalf("envelope round trip changed cursor: %#v", decoded)
	}
}

func TestRealtimeEnvelopeRejectsUnknownSchemaAndMissingCursor(t *testing.T) {
	base := RealtimeEnvelope{
		SchemaVersion: RealtimeSchemaVersion,
		Type:          RealtimeSnapshot,
		MessageID:     "ws-1",
		OccurredAt:    time.Now().UTC(),
		Source:        "api",
		Epoch:         "epoch-1",
		Sequence:      1,
		Payload:       json.RawMessage(`{"snapshot":{}}`),
	}
	unknown := base
	unknown.SchemaVersion = "synora.realtime.v2"
	if err := unknown.Validate(); err == nil {
		t.Fatal("unknown schema version should be rejected")
	}
	missing := base
	missing.Sequence = 0
	if err := missing.Validate(); err == nil {
		t.Fatal("missing sequence should be rejected")
	}
}
