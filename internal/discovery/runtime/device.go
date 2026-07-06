package runtime

import (
	"log"
	"sync"
	"time"
)

type Device struct {
	ID string

	Type string

	IP  string
	MAC string

	Online bool

	LastSeen time.Time

	LastClip time.Time
}

type Registry struct {
	mu sync.RWMutex

	devices map[string]*Device
}

func NewRegistry() *Registry {
	return &Registry{
		devices: map[string]*Device{},
	}
}

func (r *Registry) TouchCameraClip(deviceID string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	device, exists := r.devices[deviceID]

	if exists {

		device.Online = true

		device.LastSeen = now

		device.LastClip = now

		return
	}

	device = &Device{
		ID: deviceID,

		Type: "camera",

		Online: true,

		LastSeen: now,

		LastClip: now,
	}

	r.devices[deviceID] = device

	log.Printf(
		"device initialized id=%s",
		deviceID,
	)
}

func (r *Registry) ForEachLocked(fn func(device *Device)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, device := range r.devices {
		fn(device)
	}
}
