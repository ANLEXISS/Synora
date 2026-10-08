package vision

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"synora/pkg/contract"
)

type clipProcessorFunc func(*ClipJob) (*WorkerResponse, error)

func (f clipProcessorFunc) Process(job *ClipJob) (*WorkerResponse, error) { return f(job) }

type clipMessagePublisher struct {
	messages []contract.Message
	err      error
}

func (p *clipMessagePublisher) Send(message contract.Message) error {
	if p.err != nil {
		return p.err
	}
	p.messages = append(p.messages, message)
	return nil
}

func TestWorkerQuarantinesLegacyOutputAndPublishesEvidenceOnly(t *testing.T) {
	publisher := &clipMessagePublisher{}
	processor := clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) {
		return &WorkerResponse{Events: []Event{{Type: contract.EventVisionClipSummaryV1, Payload: map[string]any{"track_id": "local-track"}}}}, nil
	})
	job := &ClipJob{ID: "clip-private", CameraID: "camera-private", EpisodeID: "episode-private", StartedAt: time.Now().UTC().Add(-time.Second), Path: "/tmp/private.mp4"}
	if err := RunClipWorker(processor, publisher, job); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("messages=%#v", publisher.messages)
	}
	message := publisher.messages[0]
	if message.Type != contract.EventVisionEvidenceV1 || message.Target != "discovery" || message.Source != "discovery" {
		t.Fatalf("unexpected worker bus message: %#v", message)
	}
	evidence, err := contract.DecodeVisionEvidenceV1(message.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Processing != "unavailable" || evidence.ErrorCode != "legacy_worker_output_quarantined" {
		t.Fatalf("legacy output was not quarantined: %#v", evidence)
	}
	for _, forbidden := range []string{"clip-private", "camera-private", "episode-private", "local-track", "/tmp/private.mp4"} {
		if string(message.Payload) == forbidden || containsString(string(message.Payload), forbidden) {
			t.Fatalf("private value leaked: %s", forbidden)
		}
	}
}

func TestWorkerErrorsProduceStructuredUnavailableEvidence(t *testing.T) {
	publisher := &clipMessagePublisher{}
	job := &ClipJob{ID: "clip-fail", CameraID: "cam", StartedAt: time.Now().UTC().Add(-time.Second), SimulatedCamera: true}
	errExpected := errors.New("decoder failed")
	if err := RunClipWorker(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) { return nil, errExpected }), publisher, job); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("messages=%#v", publisher.messages)
	}
	evidence, err := contract.DecodeVisionEvidenceV1(publisher.messages[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Processing != "unavailable" || evidence.ProducerHealth != "unavailable" || !evidence.SimulatedCamera || evidence.Provenance != "simulated_test" || evidence.ErrorCode != "worker_unavailable" {
		t.Fatalf("bad unavailable evidence: %#v", evidence)
	}
}

func TestRetryAttemptDoesNotPublishUntilTerminalEvidence(t *testing.T) {
	publisher := &clipMessagePublisher{}
	job := &ClipJob{ID: "clip-retry", CameraID: "cam", StartedAt: time.Now().UTC().Add(-time.Second)}
	errExpected := errors.New("temporary")
	if err := RunClipWorkerAttempt(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) { return nil, errExpected }), publisher, job); !errors.Is(err, errExpected) {
		t.Fatalf("attempt error=%v", err)
	}
	if len(publisher.messages) != 0 {
		t.Fatalf("retry attempt leaked intermediate message: %#v", publisher.messages)
	}
	if err := PublishClipFailure(publisher, job, "worker_timeout"); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 {
		t.Fatalf("terminal messages=%#v", publisher.messages)
	}
	evidence, err := contract.DecodeVisionEvidenceV1(publisher.messages[0].Payload)
	if err != nil || evidence.ErrorCode != "worker_timeout" {
		t.Fatalf("terminal result=%#v err=%v", evidence, err)
	}
}

func TestWorkerAcceptsOnlyValidatedEvidenceV1(t *testing.T) {
	publisher := &clipMessagePublisher{}
	evidence := minimalWorkerEvidence(t, false)
	job := &ClipJob{ID: "job", CameraID: "cam", StartedAt: evidence.WindowStart}
	if err := RunClipWorker(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) { return &WorkerResponse{VisionEvidence: &evidence}, nil }), publisher, job); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 1 || publisher.messages[0].Type != contract.EventVisionEvidenceV1 {
		t.Fatalf("messages=%#v", publisher.messages)
	}
	if _, err := contract.DecodeVisionEvidenceV1(publisher.messages[0].Payload); err != nil {
		t.Fatal(err)
	}
}

func minimalWorkerEvidence(t *testing.T, simulated bool) contract.VisionEvidenceV1 {
	t.Helper()
	var e contract.VisionEvidenceV1
	if err := json.Unmarshal([]byte(`{"schema_version":"synora.vision.evidence/v1","event_id":"ev_0123456789abcdef01234567","episode_id":"ep_0123456789abcdef01234567","window_start":"2026-01-01T00:00:00Z","window_end":"2026-01-01T00:00:01Z","window_seconds":1,"topology":"unknown","provenance":"real","simulated_camera":false,"camera_health":{"availability":"unavailable","state":"unavailable","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"trigger":{"availability":"unavailable","state":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"presence":{"human":{"availability":"unavailable","state":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"vehicle":{"availability":"unavailable","state":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"animal":{"availability":"unavailable","state":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}}},"activity":{"availability":"unavailable","state":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"pose":{"availability":"unavailable","posture":"unknown","posture_confidence":0,"transition_to_ground_confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0},"immobility_seconds":0},"face":{"availability":"unavailable","result":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"plate":{"availability":"unavailable","result":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"sensitive_object":{"availability":"unavailable","category":"unknown","confidence":0,"quality":0,"support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"media":{"availability":"unavailable","episode_state":"unknown","support":{"valid_evaluations":0,"continuity":"unknown","supported_seconds":0,"gap_count":0}},"producer_health":"unavailable","processing_status":"unavailable"}`), &e); err != nil {
		t.Fatal(err)
	}
	if simulated {
		e.Provenance, e.SimulatedCamera = "simulated_test", true
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	return e
}

func containsString(value, needle string) bool {
	return needle != "" && len(value) >= len(needle) && json.Valid([]byte(`"`+value+`"`)) && contains(value, needle)
}
func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
