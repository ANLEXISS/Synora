package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
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
	"synora/internal/runtimeconfig"
	"synora/internal/security"
	"synora/pkg/contract"
)

type Manager struct {
	bus *bus.Client

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
					return configured.Enabled && configured.DeletedAt == nil
				}
			}
			return false
		},
	}

	workerManager := vision.NewWorkerManager(
		busClient,
		vision.WorkerManagerConfig{},
	)

	m := &Manager{
		bus: busClient,

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

		auth: auth,
	}
	faceRoot := runtime.Paths.FaceDataRoot
	if strings.TrimSpace(os.Getenv("SYNORA_FACE_DATA_ROOT")) == "" && strings.TrimSpace(cfg.Vision.FaceDataRoot) != "" {
		faceRoot = strings.TrimSpace(cfg.Vision.FaceDataRoot)
	}
	m.faceStore = facestore.New(faceRoot, facestore.Limits{})
	workerManager.SetEnvironment("SYNORA_FACE_DATA_ROOT", m.faceStore.Root)
	m.faceBuilder = facedataset.NewBuilder(m.faceStore)

	m.pool = vision.NewWorkerPool(
		4,
		func(job *vision.ClipJob) error {
			return vision.RunClipWorker(
				m.vision,
				m.bus,
				job,
			)
		},
	)

	return m
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
	go m.monitorVisionHealth(ctx)

	clipDir := runtime.Paths.ClipRoot
	m.ingressServer = ingress.StartServer(ingress.Config{
		Addr:          runtime.Endpoints.VisionHTTPS,
		CertFile:      runtime.Paths.TLSCert,
		KeyFile:       runtime.Paths.TLSKey,
		ClipDir:       clipDir,
		MaxClipSize:   MaxClipSize,
		MaxClipCount:  clipLimitInt("SYNORA_CLIP_MAX_COUNT", 500),
		MaxClipBytes:  clipLimitInt64("SYNORA_CLIP_MAX_BYTES", 5<<30),
		TempMaxAge:    clipDuration("SYNORA_CLIP_PART_MAX_AGE", time.Hour),
		Authenticator: m,
		Devices:       m.devices,
		Queue:         m.pool,
		Publisher:     m.bus,
		AllowInsecure: allowInsecureIngress(),
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
			case "resident.face_photo.updated", "resident.face_photo.removal_pending", "resident.updated":
				m.requestFaceSync()
			}
		}
	}
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
			if err := m.pool.Enqueue(&vision.ClipJob{ID: value.ID, CameraID: value.CameraID, Path: path, CreatedAt: value.CreatedAt, ActivationID: value.ActivationID, ClipIndex: value.ClipIndex, NodeID: value.NodeID, SequenceKey: value.SequenceKey, TrackID: value.TrackID}); err != nil {
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
		"models": models,
	})
}

func regularFilePath(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func statusForDiscovery(status *discoveryHealth) string {
	if status == nil {
		return "degraded"
	}
	if status.NetworkStatus == "degraded" || status.VisionWorkerStatus != "ok" || status.VisionIngressStatus != "ok" {
		return "degraded"
	}
	return "ok"
}

func (m *Manager) monitorVisionHealth(ctx context.Context) {
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
			snapshot := m.vision.Snapshot()
			status, reason := classifyVisionWorkerStatus(snapshot, missingVisionModel())
			changed := healthState.setVisionWorker(status, reason)
			if status == "unavailable" {
				m.vision.PublishUnavailable(snapshot.Status)
			}
			if changed {
				m.publishRuntimeStatus()
			}
		}
	}
}

func classifyVisionWorkerStatus(snapshot vision.WorkerSnapshot, modelsMissing bool) (string, string) {
	switch snapshot.Status {
	case vision.WorkerStatusRunning:
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
