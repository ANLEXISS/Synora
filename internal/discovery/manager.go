package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"synora/internal/bus"
	"synora/internal/clipstore"
	"synora/internal/device"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/network"
	discoveryruntime "synora/internal/discovery/runtime"
	"synora/internal/discovery/vision"
	"synora/internal/facedataset"
	"synora/internal/facestore"
	"synora/internal/mediamtx"
	"synora/internal/runtimeconfig"
	"synora/internal/security"
	"synora/pkg/contract"
)

type Manager struct {
	bus            *bus.Client
	clock          func() time.Time
	actionExecutor ActionExecutor
	actionMu       sync.Mutex
	actionResults  map[string]contract.Event

	pool *vision.WorkerPool

	vision *vision.Runtime

	workerManager *vision.WorkerManager

	devices *discoveryruntime.Registry

	auth *security.DeviceVerifier

	network *network.Manager

	healthServer  *http.Server
	ingressServer *http.Server

	faceStore     *facestore.Store
	faceBuilder   *facedataset.Builder
	faceSyncMu    sync.Mutex
	faceSyncRun   bool
	faceSyncAgain bool

	snapshotCache *SnapshotCache
	apiServer     *externalAPIServer
	securityCfg   *security.Config

	stateMu          sync.Mutex
	activeIngress    int
	stateResetting   bool
	clipRoot         string
	testVisionWorker bool
	migrationMu      sync.Mutex
	migrationMetrics VisionMigrationMetrics
}

type VisionMigrationMetrics struct {
	LegacyConverted   uint64 `json:"legacy_converted"`
	LegacyRejected    uint64 `json:"legacy_rejected"`
	LegacyQuarantined uint64 `json:"legacy_quarantined"`
}

func NewManager(
	busClient *bus.Client,
) *Manager {
	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	securityPath := runtime.Paths.Security
	cfg, err := security.Load(
		securityPath,
	)

	if err != nil {

		log.Fatal(err)
	}
	devicePath := runtime.Paths.Devices
	identityRegistry := security.NewIdentityRegistry(runtime.Paths.IdentityRegistry)
	if err := identityRegistry.Load(); err != nil {
		log.Fatal("camera identity registry: ", err)
	}

	log.Printf(
		"loaded device secrets=%d",
		len(cfg.DeviceSecrets),
	)

	auth := &security.DeviceVerifier{
		Config: func() (*security.Config, error) {
			return security.Load(securityPath)
		},
		// The durable device registry is the source of truth for trust. Reloading
		// it at ingress time ensures a deleted camera cannot keep submitting clips
		// with a still-valid transport secret until discovery is restarted.
		DeviceAllowed: func(deviceID string) bool {
			configs, err := device.Load(devicePath)
			if err != nil {
				return false
			}
			for _, configured := range configs {
				if configured.ID == deviceID {
					if !configured.Enabled || configured.DeletedAt != nil || !configured.Trusted {
						return false
					}
					trust, _ := configured.Network["network_trust"].(string)
					return strings.TrimSpace(trust) == "paired"
				}
			}
			return false
		},
		IdentityStore: identityRegistry,
	}

	workerManager := vision.NewWorkerManager(
		busClient,
		vision.WorkerManagerConfig{},
	)

	m := &Manager{
		bus:   busClient,
		clock: func() time.Time { return time.Now().UTC() },

		network: network.NewManager(),

		workerManager: workerManager,

		vision: vision.NewRuntimeWithManagerAndSocketTimeout(
			workerManager,
			runtime.Paths.VisionWorkerSocket,
			runtime.Timeouts.VisionWorker,
		),

		devices: discoveryruntime.NewRegistry(
			busClient,
		),

		auth:          auth,
		securityCfg:   cfg,
		snapshotCache: NewSnapshotCache(),
		actionResults: make(map[string]contract.Event),
		clipRoot:      runtime.Paths.ClipRoot,
	}
	faceRoot := runtime.Paths.FaceDataRoot
	if strings.TrimSpace(os.Getenv("SYNORA_FACE_DATA_ROOT")) == "" && strings.TrimSpace(cfg.Vision.FaceDataRoot) != "" {
		faceRoot = strings.TrimSpace(cfg.Vision.FaceDataRoot)
	}
	m.faceStore = facestore.New(faceRoot, facestore.Limits{})
	workerManager.SetEnvironment("SYNORA_FACE_DATA_ROOT", m.faceStore.Root)
	m.faceBuilder = facedataset.NewBuilder(m.faceStore)

	m.pool = vision.NewWorkerPoolWithConfig(
		4,
		func(job *vision.ClipJob) error {
			return vision.RunClipWorkerAttempt(
				m.vision,
				m.bus,
				job,
			)
		},
		vision.WorkerPoolConfig{
			PersistencePath: filepath.Join(runtime.Paths.ClipRoot, ".vision-queue.json"),
			OnPermanentFailure: func(job *vision.ClipJob, err error) {
				if m.bus != nil {
					_ = vision.PublishClipFailure(m.bus, job, "vision_processing_failed")
				}
			},
		},
	)
	if pending, markerErr := m.hasResetMarker(); markerErr != nil {
		log.Fatal("discovery reset marker unavailable: ", markerErr)
	} else if pending {
		recoveryCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		recoveryErr := m.ResetData(recoveryCtx)
		cancel()
		if recoveryErr != nil {
			log.Fatal("discovery data reset recovery failed: ", recoveryErr)
		}
		log.Printf("discovery recovered interrupted data reset")
	}

	return m
}

// NewTestOnlyManager constructs only the bus boundary required by the
// hermetic central integration suite. It must never start production workers.
func NewTestOnlyManager(busClient *bus.Client) *Manager {
	if os.Getenv("SYNORA_TEST_VISION_WORKER") != "1" || busClient == nil {
		return nil
	}
	return &Manager{
		bus: busClient, clock: func() time.Time { return time.Now().UTC() },
		actionResults: make(map[string]contract.Event), snapshotCache: NewSnapshotCache(),
		testVisionWorker: true,
	}
}

// StartBusOnlyContext starts the real Discovery bus boundary without opening
// HTTP, HTTPS, MediaMTX, camera, or network services. It is used by the
// central E2E harness to exercise the production Discovery action and ingress
// handlers over the real Unix bus in a temporary filesystem.
func (m *Manager) StartBusOnlyContext(ctx context.Context) {
	if m == nil || m.bus == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go m.listenFaceMutations(ctx)
}

// SetClock makes Discovery's dry-run action result deterministic for the
// central harness. Production callers leave the default wall clock intact.
func (m *Manager) SetClock(now func() time.Time) {
	if m != nil && now != nil {
		m.clock = now
	}
}

// ActionExecutor is the narrow Discovery-side action boundary. Production
// keeps the Boundary implementation; the central harness may install a
// test-only dry-run executor without bypassing Discovery's action ingress.
type ActionExecutor interface {
	ExecuteAction(ActionRequest) (contract.Event, error)
}

// SetActionExecutor is intended for hermetic central-harness replays. It does
// not expose a physical device adapter and must never be used to enable
// hardware execution.
func (m *Manager) SetActionExecutor(executor ActionExecutor) {
	if m != nil {
		m.actionExecutor = executor
	}
}

func (m *Manager) Start() {
	m.StartContext(context.Background())
}

func (m *Manager) StartContext(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	go discoveryruntime.StartLoopContext(ctx,
		m.devices,
		m.bus,
	)
	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		log.Printf("runtime configuration unavailable: %v", err)
		return
	}
	go m.superviseMediaMTX(ctx, runtime.Paths.Devices, runtime.Endpoints.MediaMTXAPIURL)
	m.healthServer = startHealthServer(runtime.Endpoints.VisionHealth)

	err = m.network.StartContext(ctx)

	if err != nil {

		log.Printf(
			"network degraded mode enabled err=%v",
			err,
		)
		healthState.setNetwork("degraded", err.Error())
		m.publishDiagnostic(contract.EventDiscoveryNetworkDegraded, map[string]any{
			"component": "network",
			"status":    "degraded",
			"reason":    err.Error(),
		})

	} else {

		log.Printf(
			"private network ready",
		)
		healthState.setNetwork("ok", "")
	}

	err = m.vision.Start()

	if err != nil {
		log.Printf("vision worker degraded mode enabled err=%v", err)
		healthState.setVisionWorker("unavailable", err.Error())
		m.publishDiagnostic(contract.EventDiscoveryVisionWorkerUnavailable, map[string]any{
			"component": "vision_worker",
			"status":    "unavailable",
			"reason":    err.Error(),
		})
	} else {
		healthState.setVisionWorker("ok", "")
	}
	healthState.setSuccess(0)
	m.refreshCameraHealth(runtime.Paths.Devices)
	go m.monitorVisionHealth(ctx, runtime.Paths.Devices)
	boundary := &Boundary{DryRun: true, Store: m.snapshotCache}
	api := NewExternalAPI(m.securityCfg, boundary, busEventPublisher{client: m.bus}, m.snapshotCache, func() map[string]any {
		status := healthState.snapshot()
		return map[string]any{"service": "discovery", "status": status.VisionWorkerStatus, "vision_worker": status.VisionWorkerStatus, "vision_ingress": status.VisionIngressStatus, "network": status.NetworkStatus}
	})
	m.apiServer = startExternalAPIServer(runtime, m.securityCfg, api)

	clipDir := runtime.Paths.ClipRoot
	m.ingressServer = ingress.StartServer(ingress.Config{
		Addr:                    runtime.Endpoints.VisionHTTPS,
		CertFile:                runtime.Paths.TLSCert,
		KeyFile:                 runtime.Paths.TLSKey,
		ClipDir:                 clipDir,
		MaxClipSize:             MaxClipSize,
		MaxClipCount:            clipLimitInt("SYNORA_CLIP_MAX_COUNT", 500),
		MaxClipBytes:            clipLimitInt64("SYNORA_CLIP_MAX_BYTES", 5<<30),
		TempMaxAge:              clipDuration("SYNORA_CLIP_PART_MAX_AGE", time.Hour),
		Authenticator:           m,
		Devices:                 m.devices,
		Queue:                   m.pool,
		Publisher:               m.bus,
		ClipDuration:            clipDuration("SYNORA_VISION_V1_MAX_DURATION", 10*time.Second),
		EpisodeContinuityWindow: clipDuration("SYNORA_VISION_V1_CONTINUITY_WINDOW", 5*time.Second),
		AllowInsecure:           allowInsecureIngress(),
		OnStatus: func(status, reason string) {
			healthState.setVisionIngress(status, reason)
			m.publishDiagnostic(contract.EventDiscoveryVisionIngressStatus, map[string]any{
				"component": "vision_ingress",
				"status":    status,
				"reason":    reason,
			})
		},
	})
	go m.resumePendingClips(ctx, clipDir)
	if err := m.faceStore.Init(); err != nil {
		log.Printf("discovery face storage degraded err=%v", err)
	}
	go m.listenFaceMutations(ctx)
	m.requestFaceSync()
	m.publishRuntimeStatus()
}

func (m *Manager) listenFaceMutations(ctx context.Context) {
	if m == nil || m.bus == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	messages := m.bus.SubscribeChannel("discovery")
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			switch msg.Type {
			case contract.RPCSystemResetState:
				m.handleSystemStateReset(msg)
			case "action.request":
				m.handleV1ActionRequest(msg)
			case EdgeTrackManifestSchemaV1:
				m.handleEdgeTrackManifest(msg)
			case contract.EventVisionEnrichmentV3:
				m.handleVisionEnrichmentV3(msg)
			case contract.EventVisionEvidenceV1:
				m.handleVisionEvidenceV1(msg)
			case "core.snapshot":
				if m.snapshotCache != nil {
					if err := m.snapshotCache.Apply(msg); err != nil {
						log.Printf("discovery snapshot update rejected: %v", err)
					}
				}
			case "resident.face_photo.updated", "resident.face_photo.removal_pending", "resident.updated", "residents.face_dataset.building":
				m.requestFaceSync()
			}
		}
	}
}

// handleV1ActionRequest is the only runtime bridge from the Core outbox to a
// device adapter. V1 remains dry-run and publishes the result as a new event;
// it never mutates the Store or calls a physical device.
func (m *Manager) handleV1ActionRequest(message contract.Message) {
	if m == nil || m.bus == nil {
		return
	}
	var request ActionRequest
	if err := json.Unmarshal(message.Payload, &request); err != nil {
		return
	}
	m.actionMu.Lock()
	if previous, ok := m.actionResults[request.RequestID]; ok {
		m.actionMu.Unlock()
		m.publishActionResult(message, previous)
		return
	}
	m.actionMu.Unlock()
	executor := ActionExecutor(&Boundary{DryRun: true, Now: m.clock})
	if m.actionExecutor != nil {
		executor = m.actionExecutor
	}
	event, err := executor.ExecuteAction(request)
	if err != nil {
		return
	}
	m.actionMu.Lock()
	if m.actionResults == nil {
		m.actionResults = make(map[string]contract.Event)
	}
	m.actionResults[request.RequestID] = event
	m.actionMu.Unlock()
	m.publishActionResult(message, event)
}

func (m *Manager) publishActionResult(message contract.Message, event contract.Event) {
	if m == nil || m.bus == nil {
		return
	}
	body, err := json.Marshal(event.Payload)
	if err != nil {
		return
	}
	// Derive the bus event identity from the request identity. Boundary's
	// internal event ID remains opaque, while hermetic replays stay stable and
	// retries cannot create a new logical action-result identity.
	_ = m.bus.Send(contract.Message{ID: message.ID + ":result", Type: event.Type, Kind: contract.KindEvent, Source: "discovery", Target: "core", CorrelationID: message.CorrelationID, Timestamp: event.Timestamp, Payload: body})
}

func (m *Manager) handleEdgeTrackManifest(message contract.Message) {
	if m == nil || m.bus == nil {
		return
	}
	manifest, err := AcceptEdgeTrackManifestAt(message.Payload, m.clock())
	if err != nil {
		m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyRejected++ })
		m.sendVisionIngressResult(message, "rejected", "edge_manifest_invalid")
		return
	}
	evidence, err := edgeManifestEvidence(message.ID, manifest)
	if err != nil {
		m.quarantineVision(message, "edge_manifest_unrepresentable")
		return
	}
	if err := m.PublishValidatedVisionEvidenceV1(evidence); err != nil {
		m.quarantineVision(message, "edge_evidence_rejected")
		return
	}
	m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyConverted++ })
	body, _ := json.Marshal(map[string]any{"schema_version": "discovery.ingress.result/v1", "status": "accepted", "vision_evidence_v1": true})
	_ = m.bus.Send(contract.Message{ID: message.ID + ":accepted", Type: "discovery.ingress.accepted", Kind: contract.KindEvent, Source: "discovery", Target: message.Source, CorrelationID: message.ID, Timestamp: m.clock(), Payload: body})
}

func (m *Manager) handleVisionEnrichmentV3(message contract.Message) {
	if m == nil || m.bus == nil {
		return
	}
	if err := ValidatePayload(message.Payload); err != nil {
		m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyRejected++ })
		m.sendVisionIngressResult(message, "rejected", "vision_payload_invalid")
		return
	}
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(message.Payload, &outer); err != nil {
		m.quarantineVision(message, "legacy_enrichment_invalid")
		return
	}
	var evidenceRaw json.RawMessage
	if rawSchema := outer["schema_version"]; len(rawSchema) > 0 {
		var schema string
		_ = json.Unmarshal(rawSchema, &schema)
		if schema == contract.EventVisionEvidenceV1 {
			evidenceRaw = message.Payload
		}
	}
	if len(evidenceRaw) == 0 {
		for key := range outer {
			if key != "schema_version" && key != "vision_evidence" {
				m.quarantineVision(message, "legacy_enrichment_unmapped_fields")
				return
			}
		}
		evidenceRaw = outer["vision_evidence"]
	}
	if len(evidenceRaw) == 0 {
		m.quarantineVision(message, "legacy_enrichment_without_evidence")
		return
	}
	evidence, err := contract.DecodeVisionEvidenceV1(evidenceRaw)
	if err != nil {
		m.quarantineVision(message, "legacy_enrichment_evidence_invalid")
		return
	}
	if m.testVisionWorker {
		var marker struct {
			Provenance           string `json:"provenance"`
			SimulatedCamera      bool   `json:"simulated_camera"`
			VisionStatus         string `json:"vision_status"`
			VisionEvidenceSource string `json:"vision_evidence_source"`
			InferenceExecuted    bool   `json:"inference_executed"`
		}
		if err := json.Unmarshal(message.Payload, &marker); err != nil || marker.Provenance != "simulated_test_worker" ||
			!marker.SimulatedCamera || marker.VisionStatus != "unavailable" ||
			marker.VisionEvidenceSource != "simulated_test_worker" || marker.InferenceExecuted {
			body, _ := json.Marshal(map[string]any{"schema_version": "discovery.ingress.result/v1", "status": "rejected", "reason": "simulated_worker_contract_invalid", "simulated_camera": true})
			_ = m.bus.Send(contract.Message{ID: message.ID + ":rejected", Type: "discovery.ingress.rejected", Kind: contract.KindEvent, Source: "discovery", Target: message.Source, CorrelationID: message.ID, Timestamp: m.clock(), Payload: body})
			return
		}
	}
	accepted := map[string]any{"schema_version": "discovery.ingress.result/v1", "status": "accepted", "vision_evidence_v1": true}
	acceptedTarget := message.Source
	if m.testVisionWorker {
		accepted["simulated_camera"] = true
		accepted["vision_evidence_source"] = "simulated_test_worker"
		accepted["inference_executed"] = false
		acceptedTarget = "camera-simulator"
	}
	body, _ := json.Marshal(accepted)
	_ = m.bus.Send(contract.Message{ID: message.ID + ":accepted", Type: "discovery.ingress.accepted", Kind: contract.KindEvent, Source: "discovery", Target: acceptedTarget, CorrelationID: message.ID, Timestamp: m.clock(), Payload: body})
	if err := m.PublishValidatedVisionEvidenceV1(evidence); err != nil {
		m.quarantineVision(message, "legacy_enrichment_evidence_rejected")
		return
	}
	m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyConverted++ })
}

// PublishValidatedVisionEvidenceV1 is the only Discovery-to-Core semantic
// publisher. The event body is the contract itself, never a legacy wrapper.
func (m *Manager) PublishValidatedVisionEvidenceV1(evidence contract.VisionEvidenceV1) error {
	if m == nil || m.bus == nil {
		return errors.New("Discovery bus unavailable")
	}
	if err := evidence.Validate(); err != nil {
		return err
	}
	body, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	if err := ValidatePayload(body); err != nil {
		return err
	}
	return m.bus.Send(contract.Message{ID: evidence.EventID, Type: contract.EventVisionEvidenceV1, Kind: contract.KindEvent, Source: "discovery", Target: "core", Timestamp: evidence.WindowEnd.UTC(), Payload: body})
}

func (m *Manager) handleVisionEvidenceV1(message contract.Message) {
	evidence, err := contract.DecodeVisionEvidenceV1(message.Payload)
	if err != nil {
		m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyRejected++ })
		m.quarantineVision(message, "evidence_v1_invalid")
		return
	}
	if m.testVisionWorker && (!evidence.SimulatedCamera || evidence.Provenance != "simulated_test" || evidence.ProducerHealth != "unavailable" || evidence.Processing != "unavailable" || evidence.ErrorCode == "") {
		m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyRejected++ })
		m.quarantineVision(message, "test_worker_evidence_invalid")
		return
	}
	if evidence.ErrorCode == "legacy_worker_output_quarantined" {
		m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyQuarantined++ })
	}
	if err := m.PublishValidatedVisionEvidenceV1(evidence); err != nil {
		m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyRejected++ })
		m.quarantineVision(message, "evidence_v1_rejected")
	}
}

func (m *Manager) quarantineVision(message contract.Message, reason string) {
	m.recordMigration(func(c *VisionMigrationMetrics) { c.LegacyQuarantined++ })
	if m == nil || m.bus == nil || message.Source == "" {
		return
	}
	m.sendVisionIngressResult(message, "quarantined", reason)
}

func (m *Manager) sendVisionIngressResult(message contract.Message, status, reason string) {
	if m == nil || m.bus == nil || message.Source == "" {
		return
	}
	body, _ := json.Marshal(map[string]any{"schema_version": "discovery.ingress.result/v1", "status": status, "reason": reason, "vision_evidence_v1": false})
	_ = m.bus.Send(contract.Message{ID: message.ID + ":" + status, Type: "discovery.ingress.rejected", Kind: contract.KindEvent, Source: "discovery", Target: message.Source, CorrelationID: message.ID, Timestamp: m.clock(), Payload: body})
}

func (m *Manager) recordMigration(update func(*VisionMigrationMetrics)) {
	if m == nil || update == nil {
		return
	}
	m.migrationMu.Lock()
	defer m.migrationMu.Unlock()
	update(&m.migrationMetrics)
}

func (m *Manager) VisionMigrationMetrics() VisionMigrationMetrics {
	if m == nil {
		return VisionMigrationMetrics{}
	}
	m.migrationMu.Lock()
	defer m.migrationMu.Unlock()
	return m.migrationMetrics
}

func edgeManifestEvidence(messageID string, manifest EdgeTrackManifestV1) (contract.VisionEvidenceV1, error) {
	if (manifest.TrackingStatus != "ok" && manifest.TrackingStatus != "unavailable") || len(manifest.EvidenceRefs) != 0 {
		return contract.VisionEvidenceV1{}, errors.New("edge semantic facts require quarantine")
	}
	for key := range manifest.Metrics {
		if key != "confidence" && key != "quality" && key != "supported_seconds" {
			return contract.VisionEvidenceV1{}, errors.New("unknown edge metric")
		}
	}
	start, err := time.Parse(time.RFC3339, manifest.StartedAt)
	if err != nil {
		return contract.VisionEvidenceV1{}, err
	}
	end, err := time.Parse(time.RFC3339, manifest.EndedAt)
	if err != nil || !end.After(start) {
		return contract.VisionEvidenceV1{}, errors.New("invalid edge window")
	}
	seconds := end.Sub(start).Seconds()
	supportedSeconds := seconds
	confidence := manifest.TriggerConfidence
	quality := confidence
	if value, ok := manifest.Metrics["confidence"].(float64); ok {
		confidence = value
	}
	if value, ok := manifest.Metrics["quality"].(float64); ok {
		quality = value
	}
	if value, ok := manifest.Metrics["supported_seconds"].(float64); ok {
		supportedSeconds = value
	}
	if confidence < 0 || confidence > 1 || quality < 0 || quality > 1 || supportedSeconds < 0 || supportedSeconds > seconds || manifest.ConfirmedTrackCount > manifest.TrackCount || manifest.ObservationCount < 1 || manifest.SegmentCount < 1 || manifest.GapCount > manifest.ObservationCount {
		return contract.VisionEvidenceV1{}, errors.New("invalid edge support values")
	}
	support := contract.VisionSupportV1{ValidEvaluations: manifest.ObservationCount, Continuity: "continuous", SupportedSeconds: supportedSeconds, GapCount: manifest.GapCount}
	if manifest.GapCount > 0 {
		support.Continuity = "gapped"
	}
	state := "absent"
	availability := contract.VisionEvaluated
	if manifest.TrackCount > 0 {
		state = "present"
	}
	if manifest.TrackingStatus == "unavailable" {
		state, availability = "unknown", contract.VisionUnavailable
	}
	if manifest.EdgeEmulated { /* marker is represented by provenance below */
	}
	sha := sha256.Sum256([]byte(messageID + "\x00" + manifest.EpisodeID))
	provenance, simulated := "real", false
	if manifest.EdgeEmulated {
		provenance, simulated = "simulated_test", true
	}
	triggerState := map[string]string{"human": "human_probable", "vehicle": "vehicle_probable", "animal": "animal_probable"}[manifest.TriggerClass]
	if triggerState == "" {
		triggerState = "unknown"
	}
	unknown := contract.VisionMeasureV1{Availability: contract.VisionNotRequested, State: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}}
	selected := contract.VisionMeasureV1{Availability: availability, State: state, Confidence: confidence, Quality: quality, Support: support}
	if availability != contract.VisionEvaluated {
		selected = contract.VisionMeasureV1{Availability: availability, State: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}}
	}
	human, vehicle, animal := unknown, unknown, unknown
	switch manifest.TriggerClass {
	case "human":
		human = selected
	case "vehicle":
		vehicle = selected
	case "animal":
		animal = selected
	}
	mediaState := "continuous"
	if manifest.GapCount > 0 {
		mediaState = "gapped"
	}
	evidence := contract.VisionEvidenceV1{SchemaVersion: contract.EventVisionEvidenceV1, EventID: "ev_" + hex.EncodeToString(sha[:12]), EpisodeID: "ep_" + hex.EncodeToString(sha[12:24]), WindowStart: start.UTC(), WindowEnd: end.UTC(), WindowSeconds: seconds, Topology: manifest.TopologyClass, Provenance: provenance, SimulatedCamera: simulated,
		CameraHealth: contract.VisionMeasureV1{Availability: contract.VisionEvaluated, State: "healthy", Confidence: confidence, Quality: quality, Support: support},
		Trigger:      contract.VisionMeasureV1{Availability: contract.VisionEvaluated, State: triggerState, Confidence: confidence, Quality: quality, Support: support},
		Presence:     contract.VisionPresenceV1{Human: human, HumanTrackCount: map[bool]int{true: manifest.TrackCount}[manifest.TriggerClass == "human"], ConfirmedHumanTracks: map[bool]int{true: manifest.ConfirmedTrackCount}[manifest.TriggerClass == "human"], Vehicle: vehicle, VehicleTrackCount: map[bool]int{true: manifest.TrackCount}[manifest.TriggerClass == "vehicle"], Animal: animal, AnimalTrackCount: map[bool]int{true: manifest.TrackCount}[manifest.TriggerClass == "animal"]},
		Activity:     contract.VisionMeasureV1{Availability: contract.VisionUnavailable, State: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}},
		Pose:         contract.VisionPoseV1{Availability: contract.VisionNotRequested, Posture: contract.VisionPostureUnknown, Support: contract.VisionSupportV1{Continuity: "unknown"}},
		Face:         contract.VisionSemanticResultV1{Availability: contract.VisionNotRequested, Result: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}}, Plate: contract.VisionSemanticResultV1{Availability: contract.VisionNotRequested, Result: "unknown", Support: contract.VisionSupportV1{Continuity: "unknown"}},
		Sensitive: contract.VisionSensitiveV1{Availability: contract.VisionNotRequested, Category: "none", Support: contract.VisionSupportV1{Continuity: "unknown"}}, Media: contract.VisionMediaContinuityV1{Availability: contract.VisionEvaluated, EpisodeState: mediaState, Support: contract.VisionSupportV1{ValidEvaluations: manifest.SegmentCount, Continuity: support.Continuity, SupportedSeconds: supportedSeconds, GapCount: manifest.GapCount}}, ProducerHealth: "healthy", Processing: "complete"}
	if len(manifest.PriorityReason) > 0 {
		evidence.PriorityReasons = append([]string(nil), manifest.PriorityReason...)
	}
	return evidence, evidence.Validate()
}

func edgePriority(topology string, human bool) string {
	if !human {
		return contract.VisionPriorityP4
	}
	switch topology {
	case contract.VisionTopologyProtectedInterior:
		return contract.VisionPriorityP1
	case contract.VisionTopologyRestrictedThreshold, contract.VisionTopologyPrivatePerimeter:
		return contract.VisionPriorityP2
	default:
		return contract.VisionPriorityP4
	}
}

func parseManifestTime(value string, fallback func() time.Time) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return parsed.UTC()
	}
	return fallback().UTC()
}

func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var errs []error
	if m.pool != nil {
		m.pool.Close()
	}
	if m.vision != nil {
		if err := m.vision.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.workerManager != nil {
		if err := m.workerManager.Stop(""); err != nil {
			errs = append(errs, err)
		}
	}
	if m.healthServer != nil {
		if err := m.healthServer.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if m.ingressServer != nil {
		if err := m.ingressServer.Shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if m.apiServer != nil {
		if err := m.apiServer.shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if m.bus != nil {
		if err := m.bus.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) requestFaceSync() {
	if m == nil {
		return
	}
	m.faceSyncMu.Lock()
	if m.faceSyncRun {
		m.faceSyncAgain = true
		m.faceSyncMu.Unlock()
		return
	}
	m.faceSyncRun = true
	m.faceSyncMu.Unlock()
	go func() {
		for {
			m.syncFaceDataset()
			m.faceSyncMu.Lock()
			again := m.faceSyncAgain
			m.faceSyncAgain = false
			if !again {
				m.faceSyncRun = false
				m.faceSyncMu.Unlock()
				return
			}
			m.faceSyncMu.Unlock()
		}
	}()
}

func (m *Manager) syncFaceDataset() {
	if m == nil || m.bus == nil || m.faceBuilder == nil || m.vision == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{"limit": 200})
	response, err := m.bus.Request("face_dataset.snapshot", "discovery", payload, "core")
	if err != nil {
		log.Printf("face dataset snapshot unavailable err=%v", err)
		return
	}
	var snapshot struct {
		DesiredRevision uint64 `json:"desired_revision"`
		Photos          []struct {
			Photo      contract.FacePhoto `json:"photo"`
			StorageKey string             `json:"storage_key"`
		} `json:"photos"`
	}
	if err := json.Unmarshal(response.Payload, &snapshot); err != nil {
		log.Printf("face dataset snapshot decode failed err=%v", err)
		return
	}
	photos := make([]contract.FacePhoto, 0, len(snapshot.Photos))
	photoByID := make(map[string]contract.FacePhoto, len(snapshot.Photos))
	for _, item := range snapshot.Photos {
		item.Photo.StorageKey = item.StorageKey
		photos = append(photos, item.Photo)
		photoByID[item.Photo.ID] = item.Photo
	}
	buildingPayload, _ := json.Marshal(map[string]any{"desired_revision": snapshot.DesiredRevision})
	if _, err := m.bus.Request("face_dataset.building", "discovery", buildingPayload, "core"); err != nil {
		log.Printf("face dataset build state unavailable err=%v", err)
		return
	}
	missing := []string{}
	for _, photo := range photos {
		if photo.Status == string(contract.FacePhotoRemoved) || photo.Status == string(contract.FacePhotoRejected) {
			continue
		}
		path, pathErr := m.faceStore.SourcePath(photo.ResidentID, photo.StorageKey)
		if pathErr != nil {
			continue
		}
		info, statErr := os.Lstat(path)
		if statErr != nil || !info.Mode().IsRegular() {
			missing = append(missing, photo.ID)
		}
	}
	if len(missing) > 0 {
		missingPayload, _ := json.Marshal(map[string]any{"photo_ids": missing})
		_, _ = m.bus.Request("face_dataset.mark_missing", "discovery", missingPayload, "core")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	manifest, err := m.faceBuilder.BuildAndActivate(ctx, photos, snapshot.DesiredRevision, m.vision, m.vision)
	if err != nil {
		log.Printf("face dataset build/reload retained previous version err=%v", err)
		var validationErr *facedataset.ValidationError
		if errors.As(err, &validationErr) && validationErr.PhotoID != "" {
			reject, _ := json.Marshal(map[string]any{"id": validationErr.PhotoID, "failure_code": validationErr.Code})
			_, _ = m.bus.Request("residents.photos.reject", "discovery", reject, "core")
		}
		failure, _ := json.Marshal(map[string]any{"failure_code": "dataset_sync_failed"})
		_, _ = m.bus.Request("face_dataset.failure", "discovery", failure, "core")
		return
	}
	residentIDs := make([]string, 0, len(manifest.Entries))
	photoIDs := make([]string, 0, len(manifest.Entries))
	seen := map[string]bool{}
	for _, entry := range manifest.Entries {
		photoIDs = append(photoIDs, entry.PhotoID)
		if !seen[entry.ResidentID] {
			residentIDs = append(residentIDs, entry.ResidentID)
			seen[entry.ResidentID] = true
		}
	}
	activate, _ := json.Marshal(map[string]any{"version": manifest.Version, "desired_revision": manifest.DesiredRevision, "manifest_checksum": manifest.Checksum, "model_fingerprint": manifest.ModelFingerprint, "embedding_dimension": manifest.EmbeddingDimension, "resident_ids": residentIDs, "photo_ids": photoIDs})
	activation, err := m.bus.Request("face_dataset.activate", "discovery", activate, "core")
	if err != nil {
		log.Printf("face dataset activation rejected err=%v", err)
		return
	}
	var activationResult struct {
		RemovedPhotoIDs []string `json:"removed_photo_ids"`
	}
	if err := json.Unmarshal(activation.Payload, &activationResult); err != nil {
		return
	}
	for _, photoID := range activationResult.RemovedPhotoIDs {
		photo, ok := photoByID[photoID]
		if !ok {
			continue
		}
		if err := m.faceStore.RemoveSource(photo); err != nil {
			log.Printf("face source removal deferred photo=%s err=%v", photoID, err)
			continue
		}
		removePayload, _ := json.Marshal(map[string]any{"id": photoID})
		if _, err := m.bus.Request("residents.photos.remove_confirmed", "discovery", removePayload, "core"); err != nil {
			log.Printf("face metadata removal confirmation failed photo=%s err=%v", photoID, err)
		}
	}
	if len(activationResult.RemovedPhotoIDs) > 0 {
		if _, err := m.faceBuilder.PurgeObsolete(); err != nil {
			log.Printf("face dataset sensitive purge deferred err=%v", err)
		}
	}
	if _, err := m.faceBuilder.PruneObsolete(7 * 24 * time.Hour); err != nil {
		log.Printf("face dataset obsolete version purge deferred err=%v", err)
	}
}

func (m *Manager) resumePendingClips(ctx context.Context, root string) {
	if m == nil || m.bus == nil || m.pool == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var beforeUpdatedAt time.Time
	var beforeID string
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		payloadValue := map[string]any{"limit": contract.MaxClipListLimit}
		if !beforeUpdatedAt.IsZero() {
			payloadValue["before_updated_at"] = beforeUpdatedAt
			payloadValue["before_id"] = beforeID
		}
		payload, _ := json.Marshal(payloadValue)
		var response *contract.Message
		var err error
		for attempt := 0; attempt < 5; attempt++ {
			select {
			case <-ctx.Done():
				return
			default:
			}
			response, err = m.bus.Request("clips.list", "discovery", payload, "core")
			if err == nil {
				break
			}
			timer := time.NewTimer(time.Second)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
		}
		if err != nil {
			log.Printf("discovery clip resume page unavailable after retries err=%v", err)
			return
		}
		var values []contract.Clip
		if err := json.Unmarshal(response.Payload, &values); err != nil {
			log.Printf("discovery clip resume decode failed err=%v", err)
			return
		}
		for _, value := range values {
			if value.Status != contract.ClipStatusReady && value.Status != contract.ClipStatusProcessing {
				continue
			}
			path, err := clipstore.FinalPath(root, value.CameraID, value.ID)
			if err != nil {
				continue
			}
			info, err := os.Lstat(path)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if err := m.pool.Enqueue(&vision.ClipJob{ID: value.ID, CameraID: value.CameraID, Path: path, CreatedAt: value.CreatedAt, ActivationID: value.ActivationID, ClipIndex: value.ClipIndex, NodeID: value.NodeID, SequenceKey: value.SequenceKey, TrackID: value.TrackID, EpisodeID: value.EpisodeID, Zone: value.Zone, TriggerReason: value.TriggerReason, StartedAt: value.StartedAt, EndsAt: value.EndsAt, Pipeline: value.Pipeline}); err != nil {
				log.Printf("discovery clip resume queue failed clip=%s err=%v", value.ID, err)
			}
		}
		if len(values) < contract.MaxClipListLimit {
			return
		}
		last := values[len(values)-1]
		if last.UpdatedAt.Equal(beforeUpdatedAt) && last.ID == beforeID {
			log.Printf("discovery clip resume cursor stalled clip=%s", last.ID)
			return
		}
		beforeUpdatedAt = last.UpdatedAt
		beforeID = last.ID
	}
}

func allowInsecureIngress() bool {
	value := strings.TrimSpace(os.Getenv("SYNORA_ALLOW_INSECURE_INGRESS"))
	allowed, _ := strconv.ParseBool(value)
	return allowed
}

func clipLimitInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func clipLimitInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func clipDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func (m *Manager) publishDiagnostic(eventType string, payload map[string]any) {
	if m == nil || m.bus == nil {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if err := m.bus.Send(contract.Message{
		Type:      eventType,
		Kind:      contract.KindEvent,
		Source:    "discovery",
		Timestamp: time.Now().UTC(),
		Payload:   body,
	}); err != nil {
		log.Printf("discovery diagnostic publish failed type=%s err=%v", eventType, err)
	}
}

func (m *Manager) publishRuntimeStatus() {
	status := healthState.snapshot()
	models := map[string]any{}
	missingModel := false
	for name, path := range map[string]string{
		"arcface": "/var/lib/synora/models/arcface_w600k_r50.rknn",
		"scrfd":   "/var/lib/synora/models/det_10g.rknn",
		"yolo":    "/var/lib/synora/models/yolov8.rknn",
		"weapon":  "/var/lib/synora/models/weapon.rknn",
	} {
		modelStatus := "present"
		if !regularFilePath(path) {
			modelStatus = "missing"
			if name != "weapon" {
				missingModel = true
			}
		}
		models[name] = map[string]any{"status": modelStatus, "path": path, "optional": name == "weapon"}
	}
	workerStatus := status.VisionWorkerStatus
	if workerStatus == "ok" && missingModel {
		workerStatus = "degraded"
		healthState.setVisionWorker("degraded", "running with missing models")
		status.VisionWorkerStatus = workerStatus
	}
	discoveryStatus := statusForDiscovery(&status)
	m.publishDiagnostic(contract.EventDiscoveryRuntimeStatus, map[string]any{
		"component": "discovery",
		"status":    discoveryStatus,
		"network":   status.NetworkStatus,
		"vision_worker": map[string]any{
			"status": workerStatus,
		},
		"vision_ingress": map[string]any{
			"status": status.VisionIngressStatus,
			"reason": status.VisionIngressError,
		},
		"mediamtx": map[string]any{
			"status": status.MediaMTXStatus,
			"reason": status.MediaMTXError,
		},
		"models": models,
	})
}

func (m *Manager) superviseMediaMTX(ctx context.Context, devicePath, apiURL string) {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := mediamtx.NewClient(apiURL, nil)
	if err != nil {
		healthState.setMediaMTX("degraded", err.Error())
		return
	}
	check := func() {
		configs, loadErr := device.Load(devicePath)
		if loadErr != nil {
			healthState.setMediaMTX("degraded", "camera configuration unavailable")
			return
		}
		ids := make([]string, 0, len(configs))
		for _, configured := range configs {
			if configured.Enabled && configured.Type == contract.DeviceTypeCamera && configured.DeletedAt == nil {
				ids = append(ids, configured.ID)
			}
		}
		desired, desiredErr := mediamtx.DesiredPaths(ids)
		if desiredErr != nil {
			healthState.setMediaMTX("degraded", desiredErr.Error())
			return
		}
		checkCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()
		report, reconcileErr := mediamtx.Reconcile(checkCtx, client, desired, time.Now().UTC())
		if reconcileErr != nil {
			healthState.setMediaMTX("degraded", report.Error)
			return
		}
		healthState.setMediaMTX("ready", "")
	}
	check()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check()
		}
	}
}

func regularFilePath(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func statusForDiscovery(status *discoveryHealth) string {
	if status == nil {
		return "degraded"
	}
	if status.NetworkStatus == "degraded" || status.VisionWorkerStatus != "ok" || status.VisionIngressStatus != "ok" || status.MediaMTXStatus == "degraded" || status.MediaMTXStatus == "unavailable" || status.MediaMTXStatus == "error" {
		return "degraded"
	}
	return "ok"
}

func (m *Manager) monitorVisionHealth(ctx context.Context, devicePath string) {
	if ctx == nil {
		ctx = context.Background()
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cameraChanged := m.refreshCameraHealth(devicePath)
			snapshot := m.vision.Snapshot()
			status, reason := classifyVisionWorkerStatus(snapshot, missingVisionModel())
			changed := healthState.setVisionWorker(status, reason)
			if status == "unavailable" {
				m.vision.PublishUnavailable(snapshot.Status)
			}
			if changed || cameraChanged {
				m.publishRuntimeStatus()
			}
		}
	}
}

func (m *Manager) refreshCameraHealth(devicePath string) bool {
	known := 0
	if configs, err := device.Load(devicePath); err == nil {
		for _, configured := range configs {
			if configured.Type == contract.DeviceTypeCamera && configured.Enabled && configured.DeletedAt == nil {
				known++
			}
		}
	}
	online := 0
	if m != nil && m.devices != nil {
		for _, observed := range m.devices.Snapshot() {
			if observed.Type == "camera" && observed.Online {
				online++
			}
		}
	}
	healthState.setSuccess(known)
	return healthState.setCameraStatus(known, online)
}

func classifyVisionWorkerStatus(snapshot vision.WorkerSnapshot, modelsMissing bool) (string, string) {
	switch snapshot.Status {
	case vision.WorkerStatusRunning:
		if snapshot.CapabilityStatus == "degraded" {
			reason := snapshot.CapabilityError
			if reason == "" {
				reason = "worker capabilities are degraded"
			}
			return "degraded", reason
		}
		if modelsMissing {
			return "degraded", "running with missing models"
		}
		return "ok", ""
	case vision.WorkerStatusStarting, vision.WorkerStatusBackoff:
		return "degraded", snapshot.Status
	case vision.WorkerStatusCrashed, vision.WorkerStatusStopped:
		return "unavailable", snapshot.Status
	default:
		return "unknown", snapshot.Status
	}
}

func missingVisionModel() bool {
	for _, path := range []string{
		"/var/lib/synora/models/arcface_w600k_r50.rknn",
		"/var/lib/synora/models/det_10g.rknn",
		"/var/lib/synora/models/yolov8.rknn",
	} {
		if !regularFilePath(path) {
			return true
		}
	}
	return false
}
