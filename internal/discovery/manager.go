package discovery

import (
	"log"
	"os"

	"synora/internal/bus"
	"synora/internal/device"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/network"
	discoveryruntime "synora/internal/discovery/runtime"
	"synora/internal/discovery/vision"
	"synora/internal/security"
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

	} else {

		log.Printf(
			"private network ready",
		)
	}

	err = m.vision.Start()

	if err != nil {

		log.Fatal(err)
	}

	ingress.StartServer(ingress.Config{
		Addr:          VisionHTTPSAddr,
		CertFile:      CertFile,
		KeyFile:       KeyFile,
		ClipDir:       VisionClipDir,
		MaxClipSize:   MaxClipSize,
		Authenticator: m,
		Devices:       m.devices,
		Queue:         m.pool,
	})
}
