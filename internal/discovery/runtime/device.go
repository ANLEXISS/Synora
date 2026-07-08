package runtime

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"synora/internal/idgen"
	"synora/pkg/contract"
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

	publisher Publisher
}

func NewRegistry(
	publisher ...Publisher,
) *Registry {
	var p Publisher
	if len(publisher) > 0 {
		p = publisher[0]
	}

	return &Registry{
		devices:   map[string]*Device{},
		publisher: p,
	}
}

func (r *Registry) TouchCameraClip(deviceID string, now time.Time) bool {
	r.mu.Lock()

	device, exists := r.devices[deviceID]

	if exists {
		wasOnline := device.Online

		device.Online = true

		device.LastSeen = now

		device.LastClip = now

		r.mu.Unlock()

		if !wasOnline {
			r.publishCameraEvent(
				contract.EventDiscoveryCameraOnline,
				deviceID,
				now,
			)
		}

		return !wasOnline
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

	r.mu.Unlock()

	r.publishCameraEvent(
		contract.EventDiscoveryCameraOnline,
		deviceID,
		now,
	)

	return true
}

func (r *Registry) ForEachLocked(fn func(device *Device)) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, device := range r.devices {
		fn(device)
	}
}

func (r *Registry) PublishCameraOffline(
	deviceID string,
	now time.Time,
) {
	r.publishCameraEvent(
		contract.EventDiscoveryCameraOffline,
		deviceID,
		now,
	)
}

func (r *Registry) publishCameraEvent(
	eventType string,
	deviceID string,
	now time.Time,
) {
	if r.publisher == nil {
		return
	}

	payload, err := json.Marshal(map[string]any{
		"camera_id": deviceID,
		"device_id": deviceID,
		"type":      "camera",
		"online":    eventType == contract.EventDiscoveryCameraOnline,
		"timestamp": now,
	})

	if err != nil {
		log.Printf(
			"camera payload error device=%s event=%s err=%v",
			deviceID,
			eventType,
			err,
		)

		return
	}

	err = r.publisher.Send(contract.Message{
		ID:        idgen.New("msg"),
		Type:      eventType,
		Kind:      contract.KindEvent,
		Source:    "discovery",
		Target:    "core",
		Timestamp: now,
		Payload:   payload,
	})

	if err != nil {
		log.Printf(
			"camera publish failed device=%s event=%s err=%v",
			deviceID,
			eventType,
			err,
		)
	}
}
