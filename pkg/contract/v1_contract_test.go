package contract

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func readV1Fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "v1", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return bytes.TrimSpace(data)
}

func assertCanonicalJSON(t *testing.T, name string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	var got, want any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode marshalled %s: %v", name, err)
	}
	if err := json.Unmarshal(readV1Fixture(t, name+"-current.json"), &want); err != nil {
		t.Fatalf("decode fixture %s: %v", name, err)
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("%s differs from golden fixture:\n got: %s\nwant: %s", name, gotJSON, wantJSON)
	}
}

func TestV1CanonicalContractsUseRFC3339AndStableValidation(t *testing.T) {
	if V1SchemaVersion != 1 || TimestampFormatV1 != "RFC3339" {
		t.Fatalf("unexpected V1 metadata: version=%d timestamp=%s", V1SchemaVersion, TimestampFormatV1)
	}
	if err := ValidateSchemaVersion(2); err == nil {
		t.Fatal("future schema version accepted")
	}
	if err := ValidateIdentifier("id", "bad id"); err == nil {
		t.Fatal("identifier containing whitespace accepted")
	}

	at := time.Date(2026, 7, 4, 10, 11, 12, 13*int(time.Millisecond), time.UTC)
	message := Message{ID: "msg-1", Version: "v1", Type: EventVisionIdentity, Source: "vision-worker", Target: "core", Timestamp: at}
	event := Event{ID: "evt-1", Type: EventVisionIdentity, Source: "vision-worker", Timestamp: at}
	decision := Decision{ID: "dec-1", Type: "intrusion.suspicious", Timestamp: at}
	action := Action{Type: "device.command", Device: "light-1", Command: "on"}
	actionRequest := ActionRequest{ID: "req-1", Type: "device.command", Target: "light-1"}
	actionResult := ActionResult{ID: "result-1", Status: ActionStatusSuccess}
	resident := Resident{ID: "resident-1", Name: "Alexis"}
	clip := Clip{ID: "clip-1", CameraID: "camera-1", Status: ClipStatusReady}
	incident := Incident{ID: "incident-1", Status: IncidentStatusNew, IdentityKind: IncidentIdentityUnknown}
	dataset := FaceDatasetVersion{
		SchemaVersion: V1SchemaVersion, Version: "dataset-1", BuiltAt: at,
		ManifestChecksum: "a", ModelFingerprint: "b", EmbeddingDimension: 512,
	}
	for name, value := range map[string]interface {
		Validate() error
	}{
		"message": message, "event": event, "decision": decision,
		"action": action, "action request": actionRequest, "action result": actionResult,
		"resident": resident, "clip": clip, "incident": incident, "dataset": dataset,
	} {
		if err := value.Validate(); err != nil {
			t.Errorf("%s rejected valid V1 contract: %v", name, err)
		}
	}

	if encoded, err := json.Marshal(message); err != nil {
		t.Fatal(err)
	} else if !bytes.Contains(encoded, []byte(`"timestamp":"2026-07-04T10:11:12.013Z"`)) {
		t.Fatalf("message timestamp is not canonical RFC3339: %s", encoded)
	}
}

func TestV1CurrentFixturesRoundTrip(t *testing.T) {
	var message Message
	if err := json.Unmarshal(readV1Fixture(t, "message-current.json"), &message); err != nil {
		t.Fatal(err)
	}
	if err := message.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "message", message)

	var event Event
	if err := json.Unmarshal(readV1Fixture(t, "event-current.json"), &event); err != nil {
		t.Fatal(err)
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "event", event)

	var decision Decision
	if err := json.Unmarshal(readV1Fixture(t, "decision-current.json"), &decision); err != nil {
		t.Fatal(err)
	}
	if err := decision.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "decision", decision)

	var clip Clip
	if err := json.Unmarshal(readV1Fixture(t, "clip-current.json"), &clip); err != nil {
		t.Fatal(err)
	}
	if err := clip.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "clip", clip)

	var resident Resident
	if err := json.Unmarshal(readV1Fixture(t, "resident-current.json"), &resident); err != nil {
		t.Fatal(err)
	}
	if err := resident.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "resident", resident)

	var actionRequest ActionRequest
	if err := json.Unmarshal(readV1Fixture(t, "action-request-current.json"), &actionRequest); err != nil {
		t.Fatal(err)
	}
	if err := actionRequest.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "action-request", actionRequest)

	var actionResult ActionResult
	if err := json.Unmarshal(readV1Fixture(t, "action-result-current.json"), &actionResult); err != nil {
		t.Fatal(err)
	}
	if err := actionResult.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "action-result", actionResult)

	var incident Incident
	if err := json.Unmarshal(readV1Fixture(t, "incident-current.json"), &incident); err != nil {
		t.Fatal(err)
	}
	if err := incident.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "incident", incident)

	var dataset FaceDatasetVersion
	if err := json.Unmarshal(readV1Fixture(t, "face-dataset-version-current.json"), &dataset); err != nil {
		t.Fatal(err)
	}
	if err := dataset.Validate(); err != nil {
		t.Fatal(err)
	}
	assertCanonicalJSON(t, "face-dataset-version", dataset)
}

func TestV1LegacyFixturesRemainReadableAndUnknownFieldsAreIgnored(t *testing.T) {
	var message Message
	if err := json.Unmarshal(readV1Fixture(t, "message-legacy.json"), &message); err != nil {
		t.Fatal(err)
	}
	if message.ID != "legacy-message" || !message.Timestamp.Equal(time.Date(2026, 7, 4, 10, 11, 12, 0, time.UTC)) {
		t.Fatalf("legacy message was not adapted: %#v", message)
	}

	var event Event
	if err := json.Unmarshal(readV1Fixture(t, "event-legacy.json"), &event); err != nil {
		t.Fatal(err)
	}
	if event.ID != "legacy-event" || event.DeviceID != "camera-1" || event.Type != EventVisionMotion {
		t.Fatalf("legacy event was not adapted: %#v", event)
	}

	var dataset FaceDatasetVersion
	if err := json.Unmarshal(readV1Fixture(t, "face-dataset-version-legacy.json"), &dataset); err != nil {
		t.Fatal(err)
	}
	if dataset.Version != "dataset-legacy" || dataset.SchemaVersion != V1SchemaVersion {
		t.Fatalf("legacy dataset was not read: %#v", dataset)
	}
	if err := dataset.Validate(); err != nil {
		t.Fatal(err)
	}
}
