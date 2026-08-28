package runtime

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"synora/pkg/contract"
)

type Device struct {
	ID string

	Type string

	IP  string
	MAC string

	Endpoint     string
	HardwareID   string
	Firmware     string
	Capabilities []string

	Online bool

	LastSeen time.Time

	LastClip time.Time
}

type Registry struct {
	mu sync.RWMutex

	devices map[string]*Device

	hardwareToCamera map[string]string

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
		devices:          map[string]*Device{},
		hardwareToCamera: map[string]string{},
		publisher:        p,
	}
}

// ObserveCamera applies one canonical technical observation and publishes it
// only when the camera's material discovery state changed. A hardware
// identity cannot be owned by two camera IDs at the same time.
func (r *Registry) ObserveCamera(observation contract.CameraObservation) error {
	if r == nil {
		return fmt.Errorf("camera registry is required")
	}
	if err := observation.EnsureID(); err != nil {
		return err
	}
	if err := observation.Validate(); err != nil {
		return err
	}
	observation = observation.Canonical()

	r.mu.Lock()
	if r.hardwareToCamera == nil {
		r.hardwareToCamera = map[string]string{}
	}
	hardwareID := observation.HardwareID
	if hardwareID != "" {
		if owner := r.hardwareToCamera[hardwareID]; owner != "" && owner != observation.CameraID {
			r.mu.Unlock()
			return fmt.Errorf("camera hardware identity %q is already owned by %q", hardwareID, owner)
		}
	}
	device := r.devices[observation.CameraID]
	if device == nil {
		device = &Device{ID: observation.CameraID, Type: "camera"}
		r.devices[observation.CameraID] = device
	}
	previous := deviceObservation(*device)
	if previous.HardwareID != "" && previous.HardwareID != hardwareID {
		delete(r.hardwareToCamera, previous.HardwareID)
	}
	device.Endpoint = observation.Endpoint
	device.IP = observation.Endpoint
	device.HardwareID = hardwareID
	device.MAC = hardwareID
	device.Firmware = observation.Firmware
	device.Capabilities = append([]string(nil), observation.Capabilities...)
	device.Online = observation.Online
	device.LastSeen = observation.LastSeen
	if hardwareID != "" {
		r.hardwareToCamera[hardwareID] = observation.CameraID
	}
	changed := !sameCameraObservation(previous, observation)
	r.mu.Unlock()
	if changed {
		return r.publishCameraObservation(observation)
	}
	return nil
}

func sameCameraObservation(left, right contract.CameraObservation) bool {
	left = left.Canonical()
	right = right.Canonical()
	return left.CameraID == right.CameraID &&
		left.HardwareID == right.HardwareID &&
		left.Endpoint == right.Endpoint &&
		left.Firmware == right.Firmware &&
		left.Online == right.Online &&
		left.LastSeen.Equal(right.LastSeen) &&
		len(left.Capabilities) == len(right.Capabilities) &&
		capabilitiesEqual(left.Capabilities, right.Capabilities)
}

func capabilitiesEqual(left, right []string) bool {
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func deviceObservation(device Device) contract.CameraObservation {
	return contract.CameraObservation{
		SchemaVersion: contract.V1SchemaVersion,
		CameraID:      device.ID,
		HardwareID:    firstNonEmpty(device.HardwareID, device.MAC),
		Endpoint:      firstNonEmpty(device.Endpoint, device.IP),
		Firmware:      device.Firmware,
		Capabilities:  append([]string(nil), device.Capabilities...),
		Online:        device.Online,
		LastSeen:      device.LastSeen,
	}.Canonical()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (r *Registry) publishCameraObservation(observation contract.CameraObservation) error {
	if r.publisher == nil {
		return nil
	}
	payload, err := json.Marshal(observation.Canonical())
	if err != nil {
		return fmt.Errorf("camera observation payload: %w", err)
	}
	return r.publisher.Send(contract.Message{
		ID:        observation.ObservationID,
		Type:      contract.EventDiscoveryCameraObserved,
		Kind:      contract.KindEvent,
		Source:    "discovery",
		Target:    "core",
		Timestamp: observation.LastSeen,
		Payload:   payload,
	})
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
		ID:        stableCameraEventID(eventType, deviceID, now),
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

func stableCameraEventID(eventType, deviceID string, now time.Time) string {
	return strings.Join([]string{"camera", eventType, deviceID, now.UTC().Format(time.RFC3339Nano)}, ":")
}

// MarkCameraOffline changes the local health projection and emits one legacy
// offline event per online-to-offline transition. Unknown cameras still emit
// an event for compatibility with the previous runtime API.
func (r *Registry) MarkCameraOffline(deviceID string, now time.Time) bool {
	r.mu.Lock()
	device, exists := r.devices[deviceID]
	wasOnline := !exists || device.Online
	if device != nil {
		device.Online = false
	}
	r.mu.Unlock()
	if wasOnline {
		r.PublishCameraOffline(deviceID, now)
	}
	return wasOnline
}

// Snapshot returns a stable copy for health checks and deterministic tests.
func (r *Registry) Snapshot() []Device {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.devices))
	for id := range r.devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Device, 0, len(ids))
	for _, id := range ids {
		if r.devices[id] == nil {
			continue
		}
		copy := *r.devices[id]
		copy.Capabilities = append([]string(nil), copy.Capabilities...)
		out = append(out, copy)
	}
	return out
}
