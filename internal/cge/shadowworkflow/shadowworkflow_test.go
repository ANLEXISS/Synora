package shadowworkflow

import (
	"context"
	"testing"
	"time"

	"synora/internal/cge/episodes"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func testInput(at time.Time, id string) ShadowWorkflowInput {
	return ShadowWorkflowInput{EventID: id, ObservedAt: at, ReceivedAt: at, Observation: episodes.ObservationRef{EventID: id, ObservedAt: at, ReceivedAt: at, EventType: "vision.identity", Subject: episodes.SubjectRef{Kind: episodes.SubjectUnknown}}, SourceShadowFingerprint: "shadow-test"}
}

func TestDisabledByDefaultHasNoRuntimeState(t *testing.T) {
	cfg := DefaultConfig()
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result := r.TrySubmit(testInput(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "disabled")); result.Status != SubmitDisabled {
		t.Fatalf("status=%s", result.Status)
	}
	if status := r.Status(); status.State != StateDisabled || status.Accepted != 0 {
		t.Fatalf("status=%+v", status)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAdvisoryPipelineCommitsDurableState(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.QueueCapacity = 4
	cfg.MaxProcessingDuration = 2 * time.Second
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	if result := r.TrySubmit(testInput(at, "event-one")); result.Status != SubmitAccepted {
		t.Fatalf("status=%s", result.Status)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r.Status().CyclesSucceeded > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	status := r.Status()
	if status.CyclesSucceeded == 0 || status.CommitsSucceeded == 0 || status.EpisodeCount != 1 {
		t.Fatalf("status=%+v metrics=%v", status, r.Metrics())
	}
	state := r.CoordinatorSnapshot()
	if len(state.Episodes) != 1 || state.Episodes[0].Episode == nil {
		t.Fatalf("state=%+v", state)
	}
	if state.Episodes[0].Freshness["advisory_requests"] == "absent" {
		t.Fatalf("advisory layer absent: %+v", state.Episodes[0].Freshness)
	}
}

func TestDuplicateEventDoesNotCreateRevision(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.MaxProcessingDuration = 2 * time.Second
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r, err := NewRuntime(context.Background(), cfg, fixedClock{now: at}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close(context.Background())
	input := testInput(at, "duplicate-event")
	if r.TrySubmit(input).Status != SubmitAccepted {
		t.Fatal("first input not accepted")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && r.Status().CyclesSucceeded == 0 {
		time.Sleep(time.Millisecond)
	}
	before := r.Status().WorkflowRevision
	if r.TrySubmit(input).Status != SubmitAccepted {
		t.Fatal("duplicate input not accepted")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && r.Status().Duplicates == 0 {
		time.Sleep(time.Millisecond)
	}
	after := r.Status()
	if after.Duplicates == 0 || after.WorkflowRevision != before {
		t.Fatalf("before=%d after=%+v", before, after)
	}
}
