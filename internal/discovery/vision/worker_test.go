package vision

import (
	"encoding/json"
	"errors"
	"testing"

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

func TestRunClipWorkerPublishesLifecycleAndStableVisionEventID(t *testing.T) {
	publisher := &clipMessagePublisher{}
	processor := clipProcessorFunc(func(job *ClipJob) (*WorkerResponse, error) {
		return &WorkerResponse{Events: []Event{{Type: contract.EventVisionUnknown, Payload: map[string]any{"clip_id": "spoofed"}}}}, nil
	})
	job := &ClipJob{ID: "clip-1", CameraID: "cam-1", ActivationID: "activation-1", SequenceKey: "sequence-1", NodeID: "entry", Path: "/tmp/clip-1.mp4"}
	if err := RunClipWorker(processor, publisher, job); err != nil {
		t.Fatal(err)
	}
	if len(publisher.messages) != 4 || publisher.messages[0].Type != contract.EventClipProcessing || publisher.messages[1].Type != contract.EventVisionUnknown || publisher.messages[2].Type != contract.EventVisionEnd || publisher.messages[3].Type != contract.EventClipProcessed {
		t.Fatalf("unexpected lifecycle messages: %#v", publisher.messages)
	}
	var payload map[string]any
	if err := json.Unmarshal(publisher.messages[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["clip_id"] != "clip-1" || payload["event_id"] != "clip-1:event:0:vision.unknown" || payload["activation_id"] != "activation-1" {
		t.Fatalf("vision metadata not stable: %#v", payload)
	}
}

func TestRunClipWorkerPublishesFailureAndDoesNotClaimProcessed(t *testing.T) {
	publisher := &clipMessagePublisher{}
	errExpected := errors.New("decoder failed")
	err := RunClipWorker(clipProcessorFunc(func(*ClipJob) (*WorkerResponse, error) { return nil, errExpected }), publisher, &ClipJob{ID: "clip-1", CameraID: "cam-1"})
	if !errors.Is(err, errExpected) {
		t.Fatalf("expected processor error, got %v", err)
	}
	if len(publisher.messages) != 2 || publisher.messages[0].Type != contract.EventClipProcessing || publisher.messages[1].Type != contract.EventClipFailed {
		t.Fatalf("unexpected failure lifecycle: %#v", publisher.messages)
	}
}
