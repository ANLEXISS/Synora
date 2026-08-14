package main

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/clipstore"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/vision"
	"synora/internal/state"
	"synora/pkg/contract"
)

type integrationClipQueue struct{ jobs []*vision.ClipJob }

func (q *integrationClipQueue) Enqueue(job *vision.ClipJob) error {
	q.jobs = append(q.jobs, job)
	return nil
}

func TestClipLifecycleTraversesIngressMetadataCoreVisionIncidentAndRestore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	clipPath, err := clipstore.FinalPath(root, "cam_01", "clip-integration")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(clipPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clipPath, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(t.TempDir(), "state.json")
	app, _ := newTestCoreApp(t)
	app.state.SetPersistence(state.NewFilePersistence(statePath))
	now := time.Date(2026, 8, 2, 18, 0, 0, 0, time.UTC)
	clip := contract.Clip{ID: "clip-integration", CameraID: "cam_01", NodeID: "entry", Status: contract.ClipStatusReady, CreatedAt: now, ReceivedAt: now, ReadyAt: now, SizeBytes: 5, Revision: 1}
	readyPayload, _ := json.Marshal(contract.ClipLifecyclePayload{Clip: clip, ClipID: clip.ID, CameraID: clip.CameraID})
	readyEvent, err := app.ingest.Parser.Parse(contract.Message{Type: contract.EventClipReady, Kind: contract.KindEvent, Source: "discovery", Timestamp: now, Payload: readyPayload})
	if err != nil {
		t.Fatal(err)
	}
	app.processEvent(readyEvent)
	stored, ok := app.state.Clip(clip.ID)
	if !ok || stored.Status != contract.ClipStatusReady || stored.Path != clipPath {
		t.Fatalf("clip was not registered honestly: %#v ok=%t", stored, ok)
	}

	visionEvent := &contract.Event{ID: "clip-integration:event:0:vision.weapon", Type: contract.EventVisionWeapon, Source: "vision-worker", Timestamp: now.Add(time.Second), DeviceID: "cam_01", NodeID: "entry", ClipID: clip.ID, ActivationID: "activation-1", SequenceKey: "sequence-1", TrackID: "track-unknown", Confidence: .9, Payload: map[string]any{"clip_id": clip.ID}}
	app.processEvent(visionEvent)
	stored, _ = app.state.Clip(clip.ID)
	if len(stored.EventIDs) != 1 || stored.EventIDs[0] != visionEvent.ID {
		t.Fatalf("clip event reference not persisted: %#v", stored)
	}
	incidents := app.state.IncidentsList(10)
	if len(incidents) != 1 || len(incidents[0].ClipIDs) != 1 || incidents[0].ClipIDs[0] != clip.ID {
		t.Fatalf("incident did not preserve real clip reference: %#v", incidents)
	}
	stored, _ = app.state.Clip(clip.ID)
	if len(stored.IncidentIDs) != 1 || stored.IncidentIDs[0] != incidents[0].ID {
		t.Fatalf("clip incident association not persisted: %#v", stored)
	}

	reloaded := state.NewStore(state.WithPersistencePath(statePath))
	if _, err := reloaded.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	restored, ok := reloaded.Clip(clip.ID)
	if !ok || len(restored.EventIDs) != 1 || len(restored.IncidentIDs) != 1 {
		t.Fatalf("clip association lost after restore: %#v ok=%t", restored, ok)
	}
}

func TestClipEndToEndUploadVisionCoreIncidentAndRestore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	statePath := filepath.Join(t.TempDir(), "state.json")
	app, bus := newTestCoreApp(t)
	app.state.SetPersistence(state.NewFilePersistence(statePath))
	queue := &integrationClipQueue{}
	handler := ingress.NewHandler(ingress.Config{ClipDir: root, MaxClipSize: 1024, Queue: queue, Publisher: bus})
	req := multipartClipRequest(t, "cam_01", "clip-e2e", []byte("video"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusAccepted || len(queue.jobs) != 1 {
		t.Fatalf("upload status=%d jobs=%d body=%s", response.Code, len(queue.jobs), response.Body.String())
	}

	processMessages := func(types ...string) {
		wanted := map[string]bool{}
		for _, value := range types {
			wanted[value] = true
		}
		for _, message := range bus.messages {
			if !wanted[message.Type] {
				continue
			}
			event, err := app.ingest.Parser.Parse(message)
			if err != nil {
				t.Fatal(err)
			}
			app.processEvent(event)
		}
	}
	processMessages(contract.EventClipReady)
	if _, ok := app.state.Clip("clip-e2e"); !ok {
		t.Fatal("Core did not register finalized upload")
	}
	processor := visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return &vision.WorkerResponse{Events: []vision.Event{{Type: contract.EventVisionWeapon, Payload: map[string]any{}}}}, nil
	})
	if err := vision.RunClipWorker(processor, bus, queue.jobs[0]); err != nil {
		t.Fatal(err)
	}
	processMessages(contract.EventClipProcessing, contract.EventVisionWeapon, contract.EventClipProcessed)
	incidents := app.state.IncidentsList(10)
	if len(incidents) != 1 || len(incidents[0].ClipIDs) != 1 || incidents[0].ClipIDs[0] != "clip-e2e" {
		t.Fatalf("end-to-end incident evidence missing: %#v", incidents)
	}
	clip, ok := app.state.Clip("clip-e2e")
	if !ok || clip.Status != contract.ClipStatusProcessed || len(clip.EventIDs) != 1 {
		t.Fatalf("end-to-end clip state=%#v ok=%t", clip, ok)
	}
	reloaded := state.NewStore(state.WithPersistencePath(statePath))
	if _, err := reloaded.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	if restored, ok := reloaded.Clip("clip-e2e"); !ok || restored.Status != contract.ClipStatusProcessed || len(restored.EventIDs) != 1 {
		t.Fatalf("end-to-end restore failed: %#v ok=%t", restored, ok)
	}
}

func TestClipProcessingRecoveryAfterRestartIsAtLeastOnceAndDeduplicated(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNORA_CLIP_DIR", root)
	statePath := filepath.Join(t.TempDir(), "state.json")
	app, bus := newTestCoreApp(t)
	app.state.SetPersistence(state.NewFilePersistence(statePath))
	queue := &integrationClipQueue{}
	handler := ingress.NewHandler(ingress.Config{ClipDir: root, MaxClipSize: 1024, Queue: queue, Publisher: bus})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, multipartClipRequest(t, "cam_01", "clip-restart", []byte("video")))
	if response.Code != http.StatusAccepted || len(queue.jobs) != 1 {
		t.Fatalf("upload status=%d jobs=%d", response.Code, len(queue.jobs))
	}
	for _, message := range bus.messagesOfType(contract.EventClipReady) {
		event, err := app.ingest.Parser.Parse(message)
		if err != nil {
			t.Fatal(err)
		}
		app.processEvent(event)
	}

	// Simulate a crash after the durable processing marker and before Vision
	// confirms completion. The restart path is exercised through Core events
	// and persistence, not by mutating StateStore directly.
	processingPayload, _ := json.Marshal(contract.ClipLifecyclePayload{
		ClipID: "clip-restart", CameraID: "cam_01",
	})
	processingEvent, err := app.ingest.Parser.Parse(contract.Message{
		ID: "clip-restart:processing", Type: contract.EventClipProcessing,
		Kind: contract.KindEvent, Source: "discovery", Timestamp: time.Now().UTC(),
		Payload: processingPayload,
	})
	if err != nil {
		t.Fatal(err)
	}
	app.processEvent(processingEvent)

	restarted, restartedBus := newTestCoreApp(t)
	restarted.state.SetPersistence(state.NewFilePersistence(statePath))
	if _, err := restarted.state.LoadPersisted(); err != nil {
		t.Fatal(err)
	}
	restarted.reconcileClips()
	stored, ok := restarted.state.Clip("clip-restart")
	if !ok || stored.Status != contract.ClipStatusReady {
		t.Fatalf("abandoned processing should recover to ready: %#v ok=%t", stored, ok)
	}

	if err := vision.RunClipWorker(visionProcessorFunc(func(*vision.ClipJob) (*vision.WorkerResponse, error) {
		return &vision.WorkerResponse{Events: []vision.Event{{Type: contract.EventVisionWeapon, Payload: map[string]any{}}}}, nil
	}), restartedBus, queue.jobs[0]); err != nil {
		t.Fatal(err)
	}
	for _, message := range restartedBus.messages {
		event, err := restarted.ingest.Parser.Parse(message)
		if err != nil {
			t.Fatal(err)
		}
		restarted.processEvent(event)
	}
	// Replaying the same at-least-once publications must not duplicate the
	// durable event or incident evidence.
	for _, message := range restartedBus.messages {
		event, err := restarted.ingest.Parser.Parse(message)
		if err != nil {
			t.Fatal(err)
		}
		restarted.processEvent(event)
	}
	stored, _ = restarted.state.Clip("clip-restart")
	incidents := restarted.state.IncidentsList(10)
	if stored.Status != contract.ClipStatusProcessed || len(stored.EventIDs) != 1 || len(incidents) != 1 || len(incidents[0].EventIDs) != 1 || len(incidents[0].ClipIDs) != 1 {
		t.Fatalf("restart replay duplicated or lost evidence clip=%#v incidents=%#v", stored, incidents)
	}
}

type visionProcessorFunc func(*vision.ClipJob) (*vision.WorkerResponse, error)

func (f visionProcessorFunc) Process(job *vision.ClipJob) (*vision.WorkerResponse, error) {
	return f(job)
}

func multipartClipRequest(t *testing.T, cameraID, clipID string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("clip", clipID+".mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/vision", &body)
	req.Header.Set("X-Synora-Device", cameraID)
	req.Header.Set("X-Synora-Clip-ID", clipID)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
