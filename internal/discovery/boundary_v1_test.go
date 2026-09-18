package discovery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBoundaryRejectsRawAndBiometricData(t *testing.T) {
	for _, payload := range []string{
		`{"bbox":[1,2,3,4]}`,
		`{"embedding":[0.1,0.2]}`,
		`{"identity":"resident"}`,
		`{"hardware_id":"secret"}`,
	} {
		if err := ValidatePayload([]byte(payload)); err == nil {
			t.Fatalf("payload accepted: %s", payload)
		}
	}
}

func TestBoundaryWebCommandIsOnlyAnEvent(t *testing.T) {
	boundary := &Boundary{DryRun: true, Now: func() time.Time { return time.Unix(10, 0) }}
	event, err := boundary.WebCommand("lock", map[string]any{"topology": "restricted_threshold"})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != WebCommandEvent || event.Source != "discovery" || !strings.Contains(event.ID, "web-") {
		t.Fatalf("unexpected Web event: %#v", event)
	}
}

func TestBoundaryActionAlwaysReturnsNonPhysicalResult(t *testing.T) {
	boundary := &Boundary{DryRun: true, Capabilities: map[string]bool{"lock": true}}
	event, err := boundary.ExecuteAction(ActionRequest{SchemaVersion: BoundarySchemaVersion, RequestID: "req-1", Action: "lock", Capability: "lock"})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != ActionResultEvent || event.Source != "discovery" {
		t.Fatalf("unexpected action result: %#v", event)
	}
	body := strings.TrimSpace(string(mustJSON(event.Payload)))
	if strings.Contains(body, `"physical_action_executed":true`) {
		t.Fatalf("physical execution reported: %s", body)
	}
}

func mustJSON(value any) []byte {
	// The helper is intentionally tiny; the production path already validates
	// through encoding/json and this test only needs a stable string check.
	return []byte(fmtJSON(value))
}

func fmtJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
