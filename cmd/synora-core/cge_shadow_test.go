package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"synora/internal/actionpolicy"
	"synora/internal/cge"
	"synora/internal/cge/durableids"
	"synora/pkg/contract"
)

type recordingCognitiveEngine struct {
	mu          sync.Mutex
	observed    []cge.Event
	observeErr  error
	shouldPanic bool
}

type coreShadowClock struct{ now time.Time }

func (c coreShadowClock) Now() time.Time { return c.now }

func (e *recordingCognitiveEngine) Observe(_ context.Context, event cge.Event) (cge.ObservationResult, error) {
	if e.shouldPanic {
		panic("test CGE panic")
	}
	e.mu.Lock()
	e.observed = append(e.observed, event)
	e.mu.Unlock()
	return cge.ObservationResult{}, e.observeErr
}

func (e *recordingCognitiveEngine) Snapshot(context.Context) (cge.Snapshot, error) {
	return cge.Snapshot{}, nil
}

func (e *recordingCognitiveEngine) Explain(_ context.Context, situationID string) (cge.Explanation, error) {
	return cge.Explanation{SituationID: situationID}, nil
}

func (e *recordingCognitiveEngine) events() []cge.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]cge.Event(nil), e.observed...)
}

func TestCoreCGEInjectionIsOptional(t *testing.T) {
	core := newTestCore(t)
	core.app.cognitive = nil

	event := &contract.Event{
		ID: "event-optional", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_02", Timestamp: time.Now().UTC(), Payload: map[string]any{"motion": true},
	}
	core.app.processEvent(event)

	if len(core.app.eventStore.List()) != 1 {
		t.Fatalf("historical processing did not continue with optional CGE")
	}
}

func TestCoreContinuesHistoricalProcessingWhenCGEObserveFails(t *testing.T) {
	core := newTestCore(t)
	core.app.cognitive = &recordingCognitiveEngine{observeErr: errors.New("shadow unavailable")}
	event := &contract.Event{
		ID: "event-error", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_02", Timestamp: time.Now().UTC(), Payload: map[string]any{"motion": true},
	}

	core.app.processEvent(event)

	if len(core.app.eventStore.List()) != 1 {
		t.Fatalf("historical processing stopped after CGE error")
	}
	if len(collectActionRequests(t, core)) != 0 {
		t.Fatalf("CGE observation produced an action request")
	}
}

func TestCorePassesCopyCapturedBeforeHistoricalEnrichment(t *testing.T) {
	core := newTestCore(t)
	observer := &recordingCognitiveEngine{}
	core.app.cognitive = observer
	event := &contract.Event{
		ID: "event-copy", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_02", Timestamp: time.Now().UTC(), Payload: map[string]any{"motion": true},
	}

	core.app.processEvent(event)

	observed := observer.events()
	if len(observed) != 1 {
		t.Fatalf("observed events = %d, want 1", len(observed))
	}
	if observed[0].NodeID != "" {
		t.Fatalf("observer saw post-processing mutation; boundary event = %#v", observed[0])
	}
	if event.NodeID != "salon" {
		t.Fatalf("historical engine did not enrich source event as expected: %#v", event)
	}

	observed[0].Type = "changed-locally"
	if event.Type != contract.EventVisionMotion {
		t.Fatalf("observer mutation changed source event type: %q", event.Type)
	}
}

func TestCoreIgnoresCGEPanic(t *testing.T) {
	core := newTestCore(t)
	core.app.cognitive = &recordingCognitiveEngine{shouldPanic: true}
	event := &contract.Event{
		ID: "event-panic", Type: contract.EventVisionMotion, Source: "vision-worker",
		DeviceID: "cam_02", Timestamp: time.Now().UTC(), Payload: map[string]any{"motion": true},
	}

	core.app.processEvent(event)
	if len(core.app.eventStore.List()) != 1 {
		t.Fatalf("historical processing did not complete with CGE panic")
	}
}

func TestCoreRunsConfiguredShadowAfterHistoricalProcessing(t *testing.T) {
	core := newTestCore(t)
	root := t.TempDir()
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = true
	config.JournalID = "core-shadow-journal"
	clock := coreShadowClock{now: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)}
	shadow, err := cge.NewShadowEngineWithConfig(context.Background(), config, clock, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("create configured shadow: %v", err)
	}
	core.app.cognitive = shadow
	defer shadow.Close()

	event := &contract.Event{
		ID: "shadow-event", Type: contract.EventVisionIdentity, Source: "vision-worker",
		DeviceID: "cam_02", NodeID: "salon", Identity: "alexis",
		Timestamp: clock.now.Add(time.Second), Payload: map[string]any{"sensitive": "must-not-reach-cge"},
	}
	core.app.processEvent(event)

	if len(core.app.eventStore.List()) != 1 {
		t.Fatalf("historical processing did not complete with configured shadow")
	}
	metrics := shadow.Metrics()
	if metrics.EventsObserved != 1 || metrics.EventsEligible != 1 || metrics.PlansCreateCandidate != 1 || metrics.AppliedCreateCandidate != 1 {
		t.Fatalf("unexpected configured shadow metrics: %#v", metrics)
	}
	if _, err := os.Stat(filepath.Join(root, "snapshots")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shadow unexpectedly created snapshots: %v", err)
	}
}

func TestCoreCGEDryRunDoesNotEmitActionRequestOrResult(t *testing.T) {
	core := newTestCore(t)
	root := t.TempDir()
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = true
	config.JournalID = "core-cge-dry-run"
	config.ExecutionMode = cge.CGEExecutionDryRun
	clock := coreShadowClock{now: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)}
	shadow, err := cge.NewShadowEngineWithConfig(context.Background(), config, clock, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("create dry-run shadow: %v", err)
	}
	defer shadow.Close()

	core.app.cognitive = shadow
	core.app.processEvent(&contract.Event{
		ID: "cge-dry-run-event", Type: contract.EventVisionUnknown, Source: "vision-worker",
		DeviceID: "cam_02", NodeID: "salon", Timestamp: clock.now,
		Payload: map[string]any{"confidence": 0.8},
	})

	if got := core.bus.messagesOfType(contract.EventActionRequest); len(got) != 0 {
		t.Fatalf("CGE dry-run emitted action requests: %#v", got)
	}
	if got := core.bus.messagesOfType(contract.EventActionResult); len(got) != 0 {
		t.Fatalf("CGE dry-run emitted action results: %#v", got)
	}
}

func TestCoreOperationalSnapshotBindsProtectedDeviceAndPolicyRevision(t *testing.T) {
	app, _ := newTestCoreApp(t)
	app.policy = actionpolicy.NewStore("")
	provider := &coreOperationalSnapshotProvider{app: app}
	target := cge.DecisionTarget{Kind: cge.DecisionTargetDevice, ID: durableids.ProtectRaw(durableids.KindDevice, "cam_01")}

	snapshot, err := provider.SnapshotForDecision(context.Background(), target)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snapshot.Revision == 0 || snapshot.PolicyRevision == 0 {
		t.Fatalf("snapshot revisions are not operationally bound: %#v", snapshot)
	}
	var deviceTarget *cge.OperationalTarget
	for i := range snapshot.Targets {
		if snapshot.Targets[i].Target == target {
			deviceTarget = &snapshot.Targets[i]
			break
		}
	}
	if deviceTarget == nil || !deviceTarget.Exists {
		t.Fatalf("protected device was not resolved: %#v", snapshot.Targets)
	}
	if deviceTarget.Authorization.Known || deviceTarget.PhysicalLimits.Known {
		t.Fatalf("unknown execution facts were fabricated: %#v", *deviceTarget)
	}
}

func TestCoreContinuesWhenConfiguredShadowIsUnavailable(t *testing.T) {
	core := newTestCore(t)
	root := t.TempDir()
	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = true
	config.JournalID = "core-shadow-journal-unavailable"
	shadow, err := cge.NewShadowEngineWithConfig(context.Background(), config, coreShadowClock{now: time.Now().UTC()}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("create configured shadow: %v", err)
	}
	if err := shadow.Close(); err != nil {
		t.Fatalf("close shadow: %v", err)
	}
	core.app.cognitive = shadow

	event := &contract.Event{
		ID: "shadow-unavailable", Type: contract.EventVisionIdentity, Source: "vision-worker",
		DeviceID: "cam_02", NodeID: "salon", Identity: "alexis", Timestamp: time.Now().UTC(),
		Payload: map[string]any{"sensitive": "must-not-reach-cge"},
	}
	core.app.processEvent(event)

	if len(core.app.eventStore.List()) != 1 {
		t.Fatalf("historical processing stopped after configured shadow failure")
	}
	if got := shadow.Metrics(); got.PlanningErrors == 0 && got.ApplyErrors == 0 {
		t.Fatalf("shadow failure was not recorded: %#v", got)
	}
}
