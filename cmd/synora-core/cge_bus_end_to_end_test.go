package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"synora/internal/bus"
	"synora/internal/cge"
	"synora/internal/cge/calibrationledger"
	cgecontext "synora/internal/cge/context"
	"synora/internal/cge/durableids"
	"synora/internal/cge/shadowworkflow"
	"synora/pkg/contract"
)

const e2eBusAt = "2026-07-22T12:00:00Z"

var e2eAt, _ = time.Parse(time.RFC3339, e2eBusAt)

type strictE2EBus struct {
	client *bus.Client

	mu       sync.Mutex
	messages []contract.Message
	actions  int
}

func (b *strictE2EBus) Send(message contract.Message) error {
	b.mu.Lock()
	b.messages = append(b.messages, message)
	if message.Type == contract.EventActionRequest || message.Kind == contract.KindCommand {
		b.actions++
		b.mu.Unlock()
		return errors.New("strict e2e action sink rejected command")
	}
	b.mu.Unlock()
	return b.client.Send(message)
}

func (b *strictE2EBus) SubscribeChannel(channel string) <-chan contract.Message {
	return b.client.SubscribeChannel(channel)
}

func (b *strictE2EBus) messagesOfType(messageType string) []contract.Message {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]contract.Message, 0)
	for _, message := range b.messages {
		if message.Type == messageType {
			out = append(out, message)
		}
	}
	return out
}

func (b *strictE2EBus) actionCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.actions
}

type busCoreE2EHarness struct {
	app         *coreApp
	shadow      *cge.ShadowEngine
	context     *coreReadOnlyContextProvider
	config      cge.ShadowConfig
	ledger      string
	socket      string
	bus         *strictE2EBus
	core        *bus.Client
	vision      *bus.Client
	server      *bus.Server
	serverErr   chan error
	stop        chan struct{}
	stopOnce    sync.Once
	processDone chan struct{}
	busDone     chan struct{}
}

func newBusCoreE2EHarness(t *testing.T) *busCoreE2EHarness {
	t.Helper()
	previousLogWriter := log.Writer()
	log.SetOutput(io.Discard)
	t.Cleanup(func() { log.SetOutput(previousLogWriter) })
	root := t.TempDir()
	socket := filepath.Join(root, "bus.sock")
	server := bus.NewServer(socket)
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.Start() }()
	waitE2EStep(t, "embedded bus Unix socket", func() bool {
		_, err := os.Stat(socket)
		select {
		case startErr := <-serverErr:
			t.Fatalf("embedded bus server stopped before readiness: %v", startErr)
		default:
		}
		return err == nil
	})

	coreClient, err := bus.NewClient(socket, "core")
	if err != nil {
		t.Fatalf("create core bus client: %v", err)
	}
	visionClient, err := bus.NewClient(socket, "vision")
	if err != nil {
		_ = coreClient.Close()
		_ = server.Close()
		t.Fatalf("create vision bus client: %v", err)
	}

	config := cge.DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = filepath.Join(root, "shadow")
	config.JournalPath = filepath.Join(config.DataDir, "historical.ndjson")
	config.InitializeIfMissing = true
	config.JournalID = "pass-61-e2e-shadow"
	config.Workflow.Enabled = true
	config.Workflow.PipelineDepth = shadowworkflow.DepthAdvisoryRequests
	config.Workflow.StoreMode = shadowworkflow.StoreFile
	config.Workflow.StoreDirectory = filepath.Join(root, "workflow")
	config.Workflow.MaxProcessingDuration = 2 * time.Second
	config.Workflow.MaxInputAge = time.Hour
	config.Workflow.CalibrationLedger.Enabled = true
	config.Workflow.CalibrationLedger.Path = filepath.Join(root, "calibration-ledger.ndjson")
	config.Workflow.CalibrationAnalytics.Enabled = false
	config.Workflow.Qualification.Enabled = false
	shadow, err := cge.NewShadowEngineWithConfig(context.Background(), config, coreShadowClock{now: e2eAt}, discardE2ELogger{})
	if err != nil {
		_ = visionClient.Close()
		_ = coreClient.Close()
		_ = server.Close()
		t.Fatalf("create configured shadow: %v", err)
	}

	app, _ := newTestCoreApp(t)
	strictBus := &strictE2EBus{client: coreClient}
	app.bus = strictBus
	app.snapshotPublisher.Bus = strictBus
	app.actionDispatcher.Bus = strictBus
	app.rpc = nil
	app.cognitive = shadow
	provider := newCoreReadOnlyContextProvider(app)
	provider.now = func() time.Time { return e2eAt }
	shadow.SetContextProvider(provider)
	stop := make(chan struct{})
	app.processStop = stop
	harness := &busCoreE2EHarness{
		app: app, shadow: shadow, context: provider, config: config, ledger: config.Workflow.CalibrationLedger.Path,
		socket: socket,
		bus:    strictBus, core: coreClient, vision: visionClient, server: server,
		serverErr: serverErr, stop: stop, processDone: make(chan struct{}), busDone: make(chan struct{}),
	}
	go func() {
		app.processLoop()
		close(harness.processDone)
	}()
	go func() {
		_ = app.runBusLoop()
		close(harness.busDone)
	}()
	t.Cleanup(harness.close)
	return harness
}

func (h *busCoreE2EHarness) close() {
	h.stopCore()
	if h.shadow != nil {
		_ = h.shadow.Close()
	}
	if h.server != nil {
		_ = h.server.Close()
	}
	if h.serverErr != nil {
		select {
		case <-h.serverErr:
		case <-time.After(2 * time.Second):
		}
	}
}

func (h *busCoreE2EHarness) stopCore() {
	h.stopOnce.Do(func() { close(h.stop) })
	if h.processDone != nil {
		<-h.processDone
		h.processDone = nil
	}
	if h.core != nil {
		_ = h.core.Close()
		h.core = nil
	}
	if h.busDone != nil {
		<-h.busDone
		h.busDone = nil
	}
	if h.vision != nil {
		_ = h.vision.Close()
		h.vision = nil
	}
}

type discardE2ELogger struct{}

func (discardE2ELogger) Printf(string, ...any) {}

func (h *busCoreE2EHarness) publish(t *testing.T, eventType string, payload map[string]any) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal bus event: %v", err)
	}
	if err := h.vision.Send(contract.Message{
		ID:   "bus-message-" + eventType,
		Type: eventType, Kind: contract.KindEvent, Source: "vision", Target: "core",
		SourceType: contract.SourceDevice, Timestamp: e2eAt, Payload: body,
	}); err != nil {
		t.Fatalf("publish %s on core channel: %v", eventType, err)
	}
}

func waitE2EStep(t *testing.T, step string, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %s", step)
		case <-ticker.C:
		}
	}
}

func coreProcessed(app *coreApp) int64 {
	value, _ := app.metrics.Snapshot(app.state)["event_processed"].(int64)
	return value
}

func mainE2EPayload(eventID, deviceID, identity, clipID string) map[string]any {
	payload := map[string]any{
		"event_id": eventID, "device_id": deviceID, "node_id": "entry", "clip_id": clipID,
		"track_id": "SENSITIVE-TRACK-SENTINEL", "confidence": 0.91,
	}
	if identity != "" {
		payload["identity"] = identity
	}
	return payload
}

func waitForE2ECommit(t *testing.T, h *busCoreE2EHarness) {
	t.Helper()
	waitE2EStep(t, "ingest, processLoop, historical processing, TrySubmit, worker and ledger commit", func() bool {
		status := h.shadow.WorkflowStatus()
		if status.StoreMode != shadowworkflow.StoreFile || !status.StorePersistent || !h.config.Workflow.SyncOnCommit {
			t.Fatalf("workflow is not using durable file storage: status=%+v config=%+v", status, h.config.Workflow)
		}
		admission := h.shadow.AdmissionStatus()
		contextStatus := h.shadow.ContextProviderStatus()
		metrics := h.shadow.Metrics()
		return coreProcessed(h.app) == 1 && len(h.app.eventStore.List()) == 1 && metrics.EventsObserved == 1 && metrics.EventsEligible == 1 && admission.AcceptedTotal == 1 && admission.LastCode == cge.ShadowAdmissionAccepted && contextStatus.Enabled && contextStatus.SnapshotsSucceeded == 1 && h.shadow.ContextProviderMetrics()["cge_core_context_snapshot_duration_ns"] > 0 && status.Accepted == 1 && status.CyclesSucceeded == 1 && status.CommitsSucceeded == 1 && status.CalibrationLedger.RecordCount == 1
	})
}

func TestBusCoreCGEDurableSensitiveIdentifierReproduction(t *testing.T) {
	h := newBusCoreE2EHarness(t)
	const (
		identity    = "PASS64-SENSITIVE-IDENTITY"
		eventID     = "PASS64-SENSITIVE-EVENT"
		deviceID    = "PASS64-SENSITIVE-DEVICE"
		clipID      = "PASS64-SENSITIVE-CLIP"
		trackID     = "PASS64-SENSITIVE-TRACK"
		activation  = "PASS64-SENSITIVE-ACTIVATION"
		sequenceKey = "PASS64-SENSITIVE-SEQUENCE"
	)
	h.publish(t, contract.EventVisionIdentity, map[string]any{
		"event_id": eventID, "device_id": deviceID, "node_id": "entry", "clip_id": clipID,
		"track_id": trackID, "activation_id": activation, "sequence_key": sequenceKey,
		"identity": identity, "confidence": 0.91,
	})
	waitForE2ECommit(t, h)
	if _, err := h.shadow.CreateCheckpoint(context.Background(), e2eAt.Add(time.Minute)); err != nil {
		t.Fatalf("create historical generation: %v", err)
	}
	h.stopCore()
	if err := h.shadow.Close(); err != nil {
		t.Fatalf("close shadow before durable scan: %v", err)
	}
	h.shadow = nil

	scanE2EDurableRoots(t, h, []string{identity, eventID, deviceID, clipID, trackID, activation, sequenceKey})
}

func scanE2EDurableRoots(t *testing.T, h *busCoreE2EHarness, sentinels []string) {
	t.Helper()
	paths := []string{h.config.DataDir, h.config.Workflow.StoreDirectory, h.config.Workflow.CalibrationLedger.Path}
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatalf("durable root %s: %v", root, err)
		}
		if info.IsDir() {
			err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil || info.IsDir() {
					return walkErr
				}
				return scanDurableJSONForSentinels(t, path, sentinels)
			})
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := scanDurableJSONForSentinels(t, root, sentinels); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBusCoreCGEDurableRedactionRejectsSpoofedTokens(t *testing.T) {
	h := newBusCoreE2EHarness(t)
	const (
		eventID     = "PASS64-2-SPOOF-EVENT"
		rawEntity   = "cgeid-v1:entity:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		rawDevice   = "cgeid-v1:entity:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		clipID      = "PASS64-2-SPOOF-CLIP"
		trackID     = "PASS64-2-SPOOF-TRACK"
		activation  = "PASS64-2-SPOOF-ACTIVATION"
		sequenceKey = "PASS64-2-SPOOF-SEQUENCE"
	)
	h.publish(t, contract.EventVisionIdentity, map[string]any{
		"event_id": eventID, "device_id": rawDevice, "node_id": "entry", "clip_id": clipID,
		"track_id": trackID, "activation_id": activation, "sequence_key": sequenceKey,
		"identity": rawEntity, "confidence": 0.91,
	})
	waitForE2ECommit(t, h)
	chains := h.shadow.ListChains()
	if len(chains) != 1 || len(chains[0].Observations) != 1 {
		t.Fatalf("unexpected spoofed-token chain snapshot: %#v", chains)
	}
	observation := chains[0].Observations[0]
	if observation.ID == eventID || observation.EntityID == rawEntity || observation.DeviceID == rawDevice {
		t.Fatalf("spoofed Core identifiers were retained: %#v", observation)
	}
	if !durableids.IsProtectedFor(durableids.KindObservation, observation.ID) ||
		!durableids.IsProtectedFor(durableids.KindEntity, observation.EntityID) ||
		!durableids.IsProtectedFor(durableids.KindDevice, observation.DeviceID) ||
		!durableids.IsProtectedFor(durableids.KindClip, observation.ClipID) ||
		!durableids.IsProtectedFor(durableids.KindTrack, observation.TrackID) ||
		!durableids.IsProtectedFor(durableids.KindActivation, observation.ActivationID) ||
		!durableids.IsProtectedFor(durableids.KindSequence, observation.SequenceKey) {
		t.Fatalf("stored observation references have incorrect domains: %#v", observation)
	}
	h.stopCore()
	if err := h.shadow.Close(); err != nil {
		t.Fatalf("close shadow before durable scan: %v", err)
	}
	h.shadow = nil
	scanE2EDurableRoots(t, h, []string{eventID, rawEntity, rawDevice, clipID, trackID, activation, sequenceKey})
}

func scanDurableJSONForSentinels(t *testing.T, path string, sentinels []string) error {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rawSentinel := ""
	for _, sentinel := range sentinels {
		if bytes.Contains(data, []byte(sentinel)) {
			rawSentinel = sentinel
			break
		}
	}
	lines := bytes.Split(data, []byte("\n"))
	for lineNumber, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var value any
		if err := json.Unmarshal(line, &value); err != nil {
			continue
		}
		var recordKind any
		if object, ok := value.(map[string]any); ok {
			recordKind = object["kind"]
			if recordKind == nil {
				recordKind = object["record_kind"]
			}
		}
		if field, sentinel, ok := findDurableSentinel(value, sentinels, "$"); ok {
			return fmt.Errorf("durable sentinel %q in file=%s record=%v line=%d json_field=%s", sentinel, path, recordKind, lineNumber+1, field)
		}
	}
	if rawSentinel != "" {
		return fmt.Errorf("durable sentinel %q in file=%s json_field=<unparsed>", rawSentinel, path)
	}
	return nil
}

func findDurableSentinel(value any, sentinels []string, field string) (string, string, bool) {
	switch typed := value.(type) {
	case string:
		for _, sentinel := range sentinels {
			if strings.Contains(typed, sentinel) {
				return field, sentinel, true
			}
		}
	case []any:
		for index, item := range typed {
			if foundField, sentinel, ok := findDurableSentinel(item, sentinels, fmt.Sprintf("%s[%d]", field, index)); ok {
				return foundField, sentinel, true
			}
		}
	case map[string]any:
		for key, item := range typed {
			if foundField, sentinel, ok := findDurableSentinel(item, sentinels, field+"."+key); ok {
				return foundField, sentinel, true
			}
		}
	}
	return "", "", false
}

func TestBusCoreCGECalibrationLedgerEndToEnd(t *testing.T) {
	h := newBusCoreE2EHarness(t)
	const eventID = "SENSITIVE-EVENT-SENTINEL"
	const identity = "SENSITIVE-IDENTITY-SENTINEL"
	const clipID = "SENSITIVE-CLIP-SENTINEL"
	const deviceID = "SENSITIVE-DEVICE-SENTINEL"
	h.publish(t, contract.EventVisionIdentity, mainE2EPayload(eventID, deviceID, identity, clipID))
	waitForE2ECommit(t, h)

	if got := h.app.eventStore.List()[0]; got.ID != eventID || got.Type != contract.EventVisionIdentity {
		t.Fatalf("ingest did not preserve the valid event: %#v", got)
	}
	decisions := h.bus.messagesOfType("engine.decision")
	if len(decisions) != 1 {
		t.Fatalf("historical Engine.Analyze decision count=%d, messages=%#v", len(decisions), decisions)
	}
	var historicalDecision contract.Decision
	if err := json.Unmarshal(decisions[0].Payload, &historicalDecision); err != nil {
		t.Fatalf("decode historical decision: %v", err)
	}
	if historicalDecision.State != h.app.state.SystemState().LastState {
		t.Fatalf("StateStore state was not produced by the historical decision: decision=%+v state=%+v", historicalDecision, h.app.state.SystemState())
	}
	if len(historicalDecision.RecommendedActionsFromCGE) != 0 {
		t.Fatalf("CGE recommendation reached the historical decision: %+v", historicalDecision)
	}
	if h.app.state.Size() == 0 || h.app.state.SystemState().LastRealEventAt.IsZero() {
		t.Fatalf("StateStore.Apply/runtime state evidence missing: %+v", h.app.state.SystemState())
	}
	if h.bus.actionCount() != 0 || len(h.bus.messagesOfType(contract.EventActionRequest)) != 0 {
		t.Fatalf("CGE or Core produced an action: count=%d", h.bus.actionCount())
	}
	admission := h.shadow.AdmissionStatus()
	if admission.LastCode != cge.ShadowAdmissionAccepted || admission.AcceptedTotal != 1 || !admission.HistoricalAuthorityUnchanged || !admission.NoActionProduced {
		t.Fatalf("unexpected end-to-end Shadow admission: %+v", admission)
	}
	if h.shadow.AdmissionMetrics()["cge_shadow_admission_accepted_total"] != 1 {
		t.Fatalf("accepted admission metric missing: %v", h.shadow.AdmissionMetrics())
	}
	contextSnapshot, err := h.context.Snapshot(context.Background(), cgecontext.SnapshotRequest{ObservationID: eventID, ObservedAt: e2eAt, NodeID: "entry"})
	if err != nil || len(contextSnapshot.Residents) == 0 || len(contextSnapshot.Devices) == 0 || len(contextSnapshot.Cameras) == 0 || contextSnapshot.Topology.Revision == "" || contextSnapshot.Freshness.Overall != cgecontext.FreshnessFresh {
		t.Fatalf("live Core context snapshot incomplete: snapshot=%+v err=%v", contextSnapshot, err)
	}
	encodedContext, _ := json.Marshal(contextSnapshot)
	for _, sentinel := range []string{identity, clipID, deviceID, eventID, "SENSITIVE-IP", "SENSITIVE-TOKEN"} {
		if strings.Contains(string(encodedContext), sentinel) {
			t.Fatalf("sensitive context sentinel persisted in snapshot: %q", sentinel)
		}
	}

	projection := h.shadow.WorkflowProjection()
	if len(projection.Situations.Situations) != 1 || len(projection.Recommendations.RecommendationSets) != 1 || len(projection.Comparisons.Comparisons) != 1 {
		t.Fatalf("workflow projections incomplete: %+v", projection)
	}
	set := projection.Recommendations.RecommendationSets[0]
	if !set.Markers.NotADecision || !set.Markers.NotAnAction || !set.Markers.NoSecurityMeaning {
		t.Fatalf("cognitive recommendation has unsafe authority markers: %+v", set.Markers)
	}
	comparison := projection.Comparisons.Comparisons[0]
	if !comparison.Markers.HistoricalDecisionRetainsAuthority || !comparison.Markers.NotAnAction || !comparison.Markers.NoSecurityMeaning {
		t.Fatalf("historical comparison authority markers invalid: %+v", comparison.Markers)
	}
	if projection.Situations.Situations[0].SourceFingerprints.Context == "" {
		t.Fatalf("cognitive situation does not carry the live context fingerprint: %+v", projection.Situations.Situations[0].SourceFingerprints)
	}
	workflowBeforeRestart := projection.Clone()
	workflowStatusBeforeRestart := h.shadow.WorkflowStatus()
	workflowDirectoryInfo, err := os.Stat(h.config.Workflow.StoreDirectory)
	if err != nil || workflowDirectoryInfo.Mode().Perm() != 0700 {
		t.Fatalf("workflow directory durability=%+v err=%v", workflowDirectoryInfo, err)
	}
	workflowWALInfo, err := os.Stat(filepath.Join(h.config.Workflow.StoreDirectory, "workflow.wal"))
	if err != nil || workflowWALInfo.Size() == 0 || workflowWALInfo.Mode().Perm() != 0600 {
		t.Fatalf("workflow WAL durability=%+v err=%v", workflowWALInfo, err)
	}
	encodedProjection, _ := json.Marshal(projection)
	for _, sentinel := range []string{identity, clipID, deviceID, eventID, "SENSITIVE-TRACK-SENTINEL", "SENSITIVE-IP", "SENSITIVE-TOKEN"} {
		if strings.Contains(string(encodedProjection), sentinel) {
			t.Fatalf("sensitive sentinel persisted in cognitive projection: %q projection=%s", sentinel, encodedProjection)
		}
	}

	records, err := h.shadow.WorkflowCalibrationRecords(calibrationledger.Query{Limit: 10})
	if err != nil || len(records.Records) != 1 || records.Matched != 1 {
		t.Fatalf("calibration record query=%+v err=%v", records, err)
	}
	record := records.Records[0]
	ledgerSnapshot := h.shadow.WorkflowCalibrationSnapshot()
	if record.Sequence != 1 || record.RecordFingerprint == "" || ledgerSnapshot.RecordCount != 1 || ledgerSnapshot.LastSequence != 1 || ledgerSnapshot.LastRecordFingerprint != record.RecordFingerprint {
		t.Fatalf("ledger sequence/fingerprint mismatch record=%+v snapshot=%+v", record, ledgerSnapshot)
	}
	if !record.Markers.HistoricalDecisionRetainsAuthority || !record.Markers.NotAProductionDecision || !record.Markers.NotAnAction || !record.Markers.NoSecurityMeaning {
		t.Fatalf("calibration record authority markers invalid: %+v", record.Markers)
	}
	raw, err := os.ReadFile(h.ledger)
	if err != nil {
		t.Fatalf("read temporary calibration ledger: %v", err)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatal("calibration ledger does not end with a newline")
	}
	for _, sentinel := range []string{identity, clipID, deviceID, eventID} {
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("sensitive sentinel persisted in calibration ledger: %q", sentinel)
		}
	}
	encodedRecord, _ := json.Marshal(record)
	if strings.Contains(string(encodedRecord), identity) || strings.Contains(string(encodedRecord), clipID) || strings.Contains(string(encodedRecord), deviceID) || strings.Contains(string(encodedRecord), eventID) {
		t.Fatalf("sensitive sentinel persisted in recovered CalibrationRecord: %s", encodedRecord)
	}

	beforeDigest := ledgerSnapshot.Digest
	beforeEnvelope := ledgerSnapshot.LastEnvelopeFingerprint
	ledgerBeforeRecovery := append([]byte(nil), raw...)
	h.stopCore()
	if err := h.shadow.Close(); err != nil {
		t.Fatalf("close first Core/Shadow/ledger runtime: %v", err)
	}
	h.shadow = nil
	reopened, err := cge.NewShadowEngineWithConfig(context.Background(), h.config, coreShadowClock{now: e2eAt}, discardE2ELogger{})
	if err != nil {
		t.Fatalf("reopen shadow and recover durable ledger: %v", err)
	}
	defer reopened.Close()
	core2Client, err := bus.NewClient(h.socket, "core-2")
	if err != nil {
		t.Fatalf("open Core 2 bus client: %v", err)
	}
	core2Bus := &strictE2EBus{client: core2Client}
	core2App, _ := newTestCoreApp(t)
	core2App.bus = core2Bus
	core2App.snapshotPublisher.Bus = core2Bus
	core2App.actionDispatcher.Bus = core2Bus
	core2App.rpc = nil
	core2App.cognitive = reopened
	core2Provider := newCoreReadOnlyContextProvider(core2App)
	core2Provider.now = func() time.Time { return e2eAt }
	reopened.SetContextProvider(core2Provider)
	core2Stop := make(chan struct{})
	core2App.processStop = core2Stop
	core2Done := make(chan struct{})
	go func() {
		_ = core2App.runBusLoop()
		close(core2Done)
	}()
	defer func() {
		close(core2Stop)
		_ = core2Client.Close()
		<-core2Done
	}()
	recoveredStatus := reopened.WorkflowStatus()
	recoveredLedger := reopened.WorkflowCalibrationSnapshot()
	if !recoveredStatus.RecoveryPerformed || !recoveredStatus.CalibrationLedger.RecoveryCompleted || recoveredLedger.RecordCount != 1 || recoveredLedger.LastSequence != 1 || recoveredLedger.Digest != beforeDigest || recoveredLedger.LastEnvelopeFingerprint != beforeEnvelope || recoveredLedger.LastRecordFingerprint != record.RecordFingerprint {
		t.Fatalf("ledger recovery changed durable identity: status=%+v snapshot=%+v", recoveredStatus, recoveredLedger)
	}
	if recoveredStatus.StoreMode != shadowworkflow.StoreFile || !recoveredStatus.StorePersistent || recoveredStatus.LastSequence != workflowStatusBeforeRestart.LastSequence || recoveredStatus.WorkflowRevision != workflowStatusBeforeRestart.WorkflowRevision || recoveredStatus.WorkflowDigest != workflowStatusBeforeRestart.WorkflowDigest || recoveredStatus.EpisodeCount != workflowStatusBeforeRestart.EpisodeCount {
		t.Fatalf("workflow recovery changed durable identity: before=%+v after=%+v", workflowStatusBeforeRestart, recoveredStatus)
	}
	recoveredProjection := reopened.WorkflowProjection()
	if !reflect.DeepEqual(recoveredProjection.Situations, workflowBeforeRestart.Situations) || !reflect.DeepEqual(recoveredProjection.Recommendations, workflowBeforeRestart.Recommendations) {
		t.Fatalf("workflow situation/recommendation projection was not reconstructed: before=%+v after=%+v", workflowBeforeRestart, recoveredProjection)
	}
	if len(recoveredProjection.Comparisons.Comparisons) != 0 {
		t.Fatalf("historical comparison was unexpectedly reconstructed without its historical reference: %+v", recoveredProjection.Comparisons)
	}
	if recoveredStatus.CommitsSucceeded != 0 || recoveredStatus.Received != 0 {
		t.Fatalf("workflow recovery produced a phantom commit: %+v", recoveredStatus)
	}
	recoveredRecords, err := reopened.WorkflowCalibrationRecords(calibrationledger.Query{Limit: 10})
	if err != nil || len(recoveredRecords.Records) != 1 || recoveredRecords.Records[0].RecordFingerprint != record.RecordFingerprint {
		t.Fatalf("recovered ledger records=%+v err=%v", recoveredRecords, err)
	}
	ledgerAfterRecovery, err := os.ReadFile(h.ledger)
	if err != nil || !bytes.Equal(ledgerBeforeRecovery, ledgerAfterRecovery) {
		t.Fatalf("workflow recovery appended to calibration ledger: err=%v before=%d after=%d", err, len(ledgerBeforeRecovery), len(ledgerAfterRecovery))
	}
}

func TestBusCoreCGECoreContextStaleIsNotNegative(t *testing.T) {
	h := newBusCoreE2EHarness(t)
	h.context.now = func() time.Time { return e2eAt.Add(20 * time.Minute) }
	h.publish(t, contract.EventVisionIdentity, mainE2EPayload("stale-context-event", "cam_01", "resident-stale", "stale-context-clip"))
	waitForE2ECommit(t, h)
	status := h.shadow.ContextProviderStatus()
	if status.StaleSnapshots != 1 || status.SnapshotsFailed != 0 || !status.Degraded {
		t.Fatalf("stale context status=%+v", status)
	}
	snapshot, err := h.context.Snapshot(context.Background(), cgecontext.SnapshotRequest{ObservationID: "stale-context-event", ObservedAt: e2eAt, NodeID: "entry"})
	if err != nil || snapshot.Freshness.Overall != cgecontext.FreshnessStale {
		t.Fatalf("stale context snapshot=%+v err=%v", snapshot, err)
	}
	var present bool
	for _, resident := range snapshot.Residents {
		if resident.PresenceCode == "present" {
			present = true
		}
	}
	if !present {
		t.Fatal("stale present context was converted into a negative fact")
	}
	if len(h.bus.messagesOfType("engine.decision")) == 0 || h.bus.actionCount() != 0 {
		t.Fatal("stale context changed historical/action behavior")
	}
}

func TestBusCoreCGEAllowlistedEvents(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		identity  string
	}{
		{name: "identity", eventType: contract.EventVisionIdentity, identity: "alexis"},
		{name: "unknown", eventType: contract.EventVisionUnknown},
		{name: "uncertain", eventType: contract.EventVisionUncertain, identity: "alexis"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newBusCoreE2EHarness(t)
			h.publish(t, tc.eventType, mainE2EPayload("evt-"+tc.name, "cam_01", tc.identity, "clip-"+tc.name))
			waitForE2ECommit(t, h)
			status := h.shadow.WorkflowStatus()
			admission := h.shadow.AdmissionStatus()
			metrics := h.shadow.Metrics()
			if status.Accepted != 1 || status.CommitsSucceeded != 1 || status.CalibrationLedger.RecordCount != 1 || admission.LastCode != cge.ShadowAdmissionAccepted || admission.AcceptedTotal != 1 || metrics.EventsEligible != 1 || len(h.shadow.WorkflowProjection().Comparisons.Comparisons) != 1 {
				t.Fatalf("allowlisted %s invariant failed: status=%+v admission=%+v metrics=%+v projection=%+v", tc.eventType, status, admission, metrics, h.shadow.WorkflowProjection())
			}
			if h.bus.actionCount() != 0 {
				t.Fatalf("allowlisted %s produced an action", tc.eventType)
			}
		})
	}
}

func TestBusCoreCGENonAllowlistedEventIsHistoricalOnly(t *testing.T) {
	h := newBusCoreE2EHarness(t)
	h.publish(t, contract.EventVisionWeapon, mainE2EPayload("evt-weapon", "cam_01", "", "clip-weapon"))
	waitE2EStep(t, "historical-only weapon event", func() bool {
		metrics := h.shadow.Metrics()
		return coreProcessed(h.app) == 1 && len(h.app.eventStore.List()) == 1 && metrics.EventsObserved == 1 && metrics.EventsSkipped == 1
	})
	metrics := h.shadow.Metrics()
	status := h.shadow.WorkflowStatus()
	admission := h.shadow.AdmissionStatus()
	if metrics.EventsEligible != 0 || metrics.EventsSkipped != 1 || admission.LastCode != cge.ShadowAdmissionIgnoredByPolicy || admission.IgnoredByPolicyTotal != 1 || status.Accepted != 0 || status.CommitsSucceeded != 0 || status.CalibrationLedger.RecordCount != 0 {
		t.Fatalf("non-allowlisted event entered Shadow workflow: metrics=%+v admission=%+v status=%+v", metrics, admission, status)
	}
	if len(h.bus.messagesOfType("engine.decision")) == 0 {
		t.Fatal("historical engine did not process non-allowlisted event")
	}
	if h.bus.actionCount() != 0 || len(h.bus.messagesOfType(contract.EventActionRequest)) != 0 {
		t.Fatal("non-allowlisted event produced an action")
	}
}
