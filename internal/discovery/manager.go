package discovery

import (
	"encoding/json"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"synora/internal/bus"
	"synora/internal/device"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/network"
	discoveryruntime "synora/internal/discovery/runtime"
	"synora/internal/discovery/vision"
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
}

func NewManager(
	busClient *bus.Client,
) *Manager {

	securityPath := os.Getenv("SYNORA_SECURITY")
	if securityPath == "" {
		securityPath = security.DefaultPath
	}
	cfg, err := security.Load(
		securityPath,
	)

	if err != nil {

		log.Fatal(err)
	}
	devicePath := os.Getenv("SYNORA_DEVICE")
	if devicePath == "" {
		devicePath = "/etc/synora/devices.yaml"
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

		vision: vision.NewRuntimeWithManager(
			workerManager,
		),

		devices: discoveryruntime.NewRegistry(
			busClient,
		),

		auth: auth,
	}

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

	go discoveryruntime.StartLoop(
		m.devices,
		m.bus,
	)

	startHealthServer()

	err := m.network.Start()

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
	go m.monitorVisionHealth()

	ingress.StartServer(ingress.Config{
		Addr:          VisionHTTPSAddr,
		CertFile:      CertFile,
		KeyFile:       KeyFile,
		ClipDir:       VisionClipDir,
		MaxClipSize:   MaxClipSize,
		Authenticator: m,
		Devices:       m.devices,
		Queue:         m.pool,
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
	m.publishRuntimeStatus()
}

func allowInsecureIngress() bool {
	value := strings.TrimSpace(os.Getenv("SYNORA_ALLOW_INSECURE_INGRESS"))
	allowed, _ := strconv.ParseBool(value)
	return allowed
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
			missingModel = true
		}
		models[name] = map[string]any{"status": modelStatus, "path": path}
	}
	workerStatus := status.VisionWorkerStatus
	if workerStatus == "ok" && missingModel {
		workerStatus = "degraded"
		healthState.setVisionWorker("degraded", "running with missing models")
		status.VisionWorkerStatus = workerStatus
	}
	discoveryStatus := statusForDiscovery(status)
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

func statusForDiscovery(status discoveryHealth) string {
	if status.NetworkStatus == "degraded" || status.VisionWorkerStatus != "ok" || status.VisionIngressStatus != "ok" {
		return "degraded"
	}
	return "ok"
}

func (m *Manager) monitorVisionHealth() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
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
		"/var/lib/synora/models/weapon.rknn",
	} {
		if !regularFilePath(path) {
			return true
		}
	}
	return false
}
