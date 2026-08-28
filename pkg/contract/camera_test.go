package contract

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCameraObservationJSONIsCanonicalAndRFC3339(t *testing.T) {
	observation := CameraObservation{
		SchemaVersion: V1SchemaVersion,
		ObservationID: "observation-1",
		CameraID:      "cam-1",
		HardwareID:    "AA:BB",
		Endpoint:      "rtsp://10.0.0.4/live",
		Firmware:      "2.4.1",
		Capabilities:  []string{"person", "weapon", "person"},
		Online:        true,
		LastSeen:      time.Date(2026, 8, 28, 10, 11, 12, 123456789, time.FixedZone("CET", 3600)),
	}
	if err := observation.EnsureID(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"observation_id":"observation-1","camera_id":"cam-1","hardware_id":"AA:BB","endpoint":"rtsp://10.0.0.4/live","firmware":"2.4.1","capabilities":["person","weapon"],"online":true,"last_seen":"2026-08-28T09:11:12.123456789Z"}`
	if string(data) != want {
		t.Fatalf("canonical JSON=%s, want %s", data, want)
	}
	if err := observation.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCameraObservationGeneratedIDIsContentStable(t *testing.T) {
	base := CameraObservation{
		CameraID:     "cam-1",
		HardwareID:   "AA:BB",
		Endpoint:     "10.0.0.4",
		Capabilities: []string{"weapon", "person"},
		Online:       true,
		LastSeen:     time.Date(2026, 8, 28, 10, 0, 0, 0, time.UTC),
	}
	other := base
	other.Capabilities = []string{"person", "weapon"}
	if err := base.EnsureID(); err != nil {
		t.Fatal(err)
	}
	if err := other.EnsureID(); err != nil {
		t.Fatal(err)
	}
	if base.ObservationID == "" || base.ObservationID != other.ObservationID {
		t.Fatalf("generated IDs differ: %q versus %q", base.ObservationID, other.ObservationID)
	}
}
