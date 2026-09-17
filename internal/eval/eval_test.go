package eval

import (
	"strings"
	"testing"
)

func TestRunMockFixtures(t *testing.T) {
	report, err := RunMock(strings.NewReader(`{"id":"one","input":{"schema_version":"cognitive/v1","request_id":"req","task":{"id":"task","kind":"observe","requested_capabilities":["event_reasoning"]},"encoded_state":{"schema_version":"state-frame/v1","encoder_id":"test","encoder_version":"1","dtype":"float32","shape":[1],"feature_names":["revision"],"values":[0],"frame_checksum":"0000000000000000000000000000000000000000000000000000000000000000"},"action_catalog":{"schema_version":"action-catalog/v1"}},"expected":{"classification":"mock_advisory","inferred_state":"unknown","requested_capabilities":["event_reasoning"],"advisory_only":true}}`))
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 0 || report.Passed != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestRunRejectsExecutableOutput(t *testing.T) {
	fixtures := `{"id":"one","input":{"schema_version":"cognitive/v1","request_id":"req","task":{"id":"task","kind":"observe"},"encoded_state":{"schema_version":"state-frame/v1","encoder_id":"test","encoder_version":"1","dtype":"float32","shape":[1],"feature_names":["revision"],"values":[0],"frame_checksum":"0000000000000000000000000000000000000000000000000000000000000000"},"action_catalog":{"schema_version":"action-catalog/v1"}},"expected":{"advisory_only":true}}`
	outputs := `{"fixture_id":"one","request_id":"req","task_id":"task","advisory_only":true,"executable_actions":[{"type":"turn_on"}]}`
	report, err := Run(strings.NewReader(fixtures), strings.NewReader(outputs))
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || report.Cases[0].Passed {
		t.Fatalf("expected rejected output: %+v", report)
	}
}

func TestRunRejectsUnknownExecutableKey(t *testing.T) {
	fixtures := `{"id":"one","input":{"schema_version":"cognitive/v1","request_id":"req","task":{"id":"task","kind":"observe"},"encoded_state":{"schema_version":"state-frame/v1","encoder_id":"test","encoder_version":"1","dtype":"float32","shape":[1],"feature_names":["revision"],"values":[0],"frame_checksum":"0000000000000000000000000000000000000000000000000000000000000000"},"action_catalog":{"schema_version":"action-catalog/v1"}},"expected":{"advisory_only":true}}`
	outputs := `{"fixture_id":"one","request_id":"req","task_id":"task","advisory_only":true,"actions":[{"id":"alert"}]}`
	report, err := Run(strings.NewReader(fixtures), strings.NewReader(outputs))
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 {
		t.Fatalf("expected unknown action key rejection: %+v", report)
	}
}
