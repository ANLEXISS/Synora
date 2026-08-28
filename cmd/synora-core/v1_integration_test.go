package main

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"synora/internal/discovery/ingress"
	"synora/internal/discovery/vision"
	"synora/internal/state"
	"synora/pkg/contract"
)

func processDiscoveryMessages(app *coreApp, bus *memoryCoreBus, cursor *int) {
	bus.mu.Lock()
	messages := append([]contract.Message(nil), bus.messages...)
	bus.mu.Unlock()
	for *cursor < len(messages) {
		message := messages[*cursor]
		(*cursor)++
		if message.Source != "discovery" {
			continue
		}
		event, err := app.ingest.Parser.Parse(message)
		if err == nil {
			app.processEvent(event)
		}
	}
}

func uploadIntegrationClip(t *testing.T, app *coreApp, bus *memoryCoreBus, queue *integrationClipQueue, cursor *int, id string) *vision.ClipJob {
	t.Helper()
	response := httptest.NewRecorder()
	handler := ingress.NewHandler(ingress.Config{
		ClipDir: clipStorageRoot(), Queue: queue, Publisher: bus, AllowInsecure: true,
		MaxClipSize: 1024 * 1024, MaxClipCount: 20, MaxClipBytes: 20 * 1024 * 1024,
	})
	handler.ServeHTTP(response, multipartClipRequest(t, "cam_01", id, []byte("video")))
	if response.Code != 202 || len(queue.jobs) == 0 {
		t.Fatalf("clip upload id=%s status=%d jobs=%d", id, response.Code, len(queue.jobs))
	}
	processDiscoveryMessages(app, bus, cursor)
	return queue.jobs[len(queue.jobs)-1]
}

func runDeterministicVision(t *testing.T, app *coreApp, bus *memoryCoreBus, job *vision.ClipJob, cursor *int, event vision.Event) {
	t.Helper()
	if err := vision.RunClipWorker(visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return &vision.WorkerResponse{Events: []vision.Event{event}}, nil
	}), bus, job); err != nil {
		t.Fatal(err)
	}
	processDiscoveryMessages(app, bus, cursor)
}

func TestV1IntegrationRecognizedResidentClipBusCorePresenceAndRestore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	app, bus := newTestCoreApp(t)
	statePath := filepath.Join(t.TempDir(), "state.json")
	app.state.SetPersistence(state.NewFilePersistence(statePath))
	queue := &integrationClipQueue{}
	cursor := 0
	job := uploadIntegrationClip(t, app, bus, queue, &cursor, "resident-clip")
	runDeterministicVision(t, app, bus, job, &cursor, vision.Event{Type: contract.EventVisionIdentity, TrackID: "resident-track", Payload: map[string]any{"resident_id": "alexis", "confidence": 0.95}})
	presence, ok := app.state.PresenceState("alexis")
	if !ok || presence.State != "present" {
		t.Fatalf("recognized resident presence missing: %#v", presence)
	}
	track, ok := app.state.ResidentTrack("alexis")
	if !ok || track.LastEventID == "" {
		t.Fatalf("resident track missing: %#v", track)
	}
	if err := app.state.SaveNow(); err != nil {
		t.Fatal(err)
	}
	restored := state.NewStore(state.WithPersistencePath(statePath))
	if _, err := restored.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	if restoredPresence, ok := restored.PresenceState("alexis"); !ok || restoredPresence.State != "present" {
		t.Fatalf("restored resident presence missing: %#v", restoredPresence)
	}
}

func TestV1IntegrationUnknownClipEntityIncidentAndDurableRestore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	app, bus := newTestCoreApp(t)
	queue := &integrationClipQueue{}
	cursor := 0
	job := uploadIntegrationClip(t, app, bus, queue, &cursor, "unknown-clip")
	runDeterministicVision(t, app, bus, job, &cursor, vision.Event{Type: contract.EventVisionUnknown, TrackID: "unknown-track", Payload: map[string]any{"confidence": 0.9}})
	if len(app.state.IncidentsList(10)) != 1 {
		t.Fatalf("unknown should create one durable incident: %#v", app.state.IncidentsList(10))
	}
	entityID := app.entityIDForEvent(&contract.Event{TrackID: "unknown-track", ActivationID: job.ActivationID, DeviceID: job.CameraID})
	if entityID == "" {
		t.Fatal("unknown entity id missing")
	}
	entity, ok := app.state.EntityTrack(entityID)
	if !ok || entity.Kind != "unknown" {
		t.Fatalf("unknown entity track missing: %#v", entity)
	}
}

func TestV1IntegrationUncertainThenIdentityBindsSameTrack(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	app, bus := newTestCoreApp(t)
	queue := &integrationClipQueue{}
	cursor := 0
	first := uploadIntegrationClip(t, app, bus, queue, &cursor, "uncertain-clip")
	runDeterministicVision(t, app, bus, first, &cursor, vision.Event{Type: contract.EventVisionUncertain, TrackID: "shared-track", Payload: map[string]any{"best_match": "Alexis", "confidence": 0.55}})
	entityID := app.entityIDForEvent(&contract.Event{TrackID: "shared-track", ActivationID: first.ActivationID, DeviceID: first.CameraID})
	entity, ok := app.state.EntityTrack(entityID)
	if !ok || entity.Kind != "uncertain" {
		t.Fatalf("uncertain anonymous track missing: %#v", entity)
	}
	second := uploadIntegrationClip(t, app, bus, queue, &cursor, "identity-clip")
	second.ActivationID, second.SequenceKey, second.TrackID = first.ActivationID, first.SequenceKey, "shared-track"
	runDeterministicVision(t, app, bus, second, &cursor, vision.Event{Type: contract.EventVisionIdentity, TrackID: "shared-track", Payload: map[string]any{"resident_id": "alexis", "confidence": 0.95}})
	entity, ok = app.state.EntityTrack(entityID)
	if !ok || entity.Kind != "resident" || entity.ResidentID != "alexis" {
		t.Fatalf("identity should bind the existing anonymous track: %#v", entity)
	}
}

func TestV1IntegrationMultiClipActivationFinalizesOnlyAfterLastClip(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	app, bus := newTestCoreApp(t)
	queue := &integrationClipQueue{}
	cursor := 0
	first := uploadIntegrationClip(t, app, bus, queue, &cursor, "activation-clip-0")
	second := uploadIntegrationClip(t, app, bus, queue, &cursor, "activation-clip-1")
	activationID := "activation-multi"
	sequenceKey := "sequence-multi"
	for index, job := range []*vision.ClipJob{first, second} {
		job.ActivationID, job.SequenceKey, job.ClipIndex = activationID, sequenceKey, index
		clip, ok := app.state.Clip(job.ID)
		if !ok {
			t.Fatalf("clip %s was not registered", job.ID)
		}
		clip.ActivationID, clip.SequenceKey, clip.ClipIndex = activationID, sequenceKey, index
		app.state.SetClip(clip)
	}
	runDeterministicVision(t, app, bus, first, &cursor, vision.Event{Type: contract.EventVisionUnknown, TrackID: "multi-track", Payload: map[string]any{"confidence": 0.9}})
	entityID := app.entityIDForEvent(&contract.Event{TrackID: "multi-track", ActivationID: activationID, SequenceKey: sequenceKey, DeviceID: first.CameraID, NodeID: "entry"})
	if entity, ok := app.state.EntityTrack(entityID); !ok || entity == nil || entity.Kind != "unknown" {
		t.Fatalf("first clip ended the multi-clip activation too early: %#v ok=%t", entity, ok)
	}
	runDeterministicVision(t, app, bus, second, &cursor, vision.Event{Type: contract.EventVisionWeapon, TrackID: "multi-track", Payload: map[string]any{"confidence": 0.95, "weapon_type": "knife"}})
	if _, ok := app.state.EntityTrack(entityID); ok {
		t.Fatal("last clip should finalize the activation entity track")
	}
	incidents := app.state.IncidentsList(10)
	if len(incidents) != 1 || len(incidents[0].ClipIDs) != 2 {
		t.Fatalf("multi-clip incident lost media correlation: %#v", incidents)
	}
}

func TestV1IntegrationTransientVisionFailureRetriesWithoutTerminalPoisoning(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	app, bus := newTestCoreApp(t)
	queue := &integrationClipQueue{}
	cursor := 0
	job := uploadIntegrationClip(t, app, bus, queue, &cursor, "retry-clip")
	transient := errors.New("temporary vision failure")
	if err := vision.RunClipWorkerAttempt(visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return nil, transient
	}), bus, job); !errors.Is(err, transient) {
		t.Fatalf("first attempt error=%v", err)
	}
	processDiscoveryMessages(app, bus, &cursor)
	clip, ok := app.state.Clip(job.ID)
	if !ok || clip.Status != contract.ClipStatusProcessing {
		t.Fatalf("transient failure poisoned clip lifecycle: %#v ok=%t", clip, ok)
	}
	if err := vision.RunClipWorkerAttempt(visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return &vision.WorkerResponse{Events: []vision.Event{{Type: contract.EventVisionUnknown, Payload: map[string]any{"confidence": 0.9}}}}, nil
	}), bus, job); err != nil {
		t.Fatal(err)
	}
	processDiscoveryMessages(app, bus, &cursor)
	clip, _ = app.state.Clip(job.ID)
	if clip.Status != contract.ClipStatusProcessed || len(app.state.IncidentsList(10)) != 1 {
		t.Fatalf("retry did not complete correlated clip: clip=%#v incidents=%#v", clip, app.state.IncidentsList(10))
	}
}
