package discovery

import (
	"log"

	"synora/internal/bus"
	"synora/internal/discovery/ingress"
	"synora/internal/discovery/network"
	discoveryruntime "synora/internal/discovery/runtime"
	"synora/internal/discovery/vision"
)

type Manager struct {
	bus *bus.Client

	pool *vision.WorkerPool

	vision *vision.Runtime

	workerManager *vision.WorkerManager

	devices *discoveryruntime.Registry

	auth *DeviceStore

	network *network.Manager
}

func NewManager(
	busClient *bus.Client,
) *Manager {

	cfg, err := LoadDevicesConfig(
		"/etc/synora/devices.yaml",
	)

	if err != nil {

		log.Fatal(err)
	}

	auth := NewDeviceStoreFromConfig(
		cfg,
	)

	log.Printf(
		"loaded devices=%d",
		len(auth.secrets),
	)

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
