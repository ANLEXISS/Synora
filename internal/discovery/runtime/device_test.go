package runtime

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"synora/pkg/contract"
)

type fakePublisher struct {
	mu       sync.Mutex
	messages []contract.Message
}

func (p *fakePublisher) Send(
	msg contract.Message,
) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.messages = append(
		p.messages,
		msg,
	)

	return nil
}

func (p *fakePublisher) count(
	eventType string,
) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	count := 0
	for _, msg := range p.messages {
		if msg.Type == eventType {
			count++
		}
	}

	return count
}

func TestRegistryPublishesCameraOnlineOncePerTransition(t *testing.T) {
	publisher := &fakePublisher{}
	registry := NewRegistry(
		publisher,
	)

	now := time.Date(
		2026,
		7,
		8,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	if !registry.TouchCameraClip("cam_01", now) {
		t.Fatal("first touch should report online transition")
	}

	if registry.TouchCameraClip("cam_01", now.Add(time.Second)) {
		t.Fatal("second touch should not report online transition")
	}

	if got := publisher.count(contract.EventDiscoveryCameraOnline); got != 1 {
		t.Fatalf("online events=%d, want 1", got)
	}
}

func TestRegistryPublishesCameraOffline(t *testing.T) {
	publisher := &fakePublisher{}
	registry := NewRegistry(
		publisher,
	)

	now := time.Date(
		2026,
		7,
		8,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	registry.PublishCameraOffline(
		"cam_01",
		now,
	)

	if got := publisher.count(contract.EventDiscoveryCameraOffline); got != 1 {
		t.Fatalf("offline events=%d, want 1", got)
	}
}

func TestRegistryObservationsAreIdempotentAndRejectHardwareCollision(t *testing.T) {
	publisher := &fakePublisher{}
	registry := NewRegistry(publisher)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	endpoints := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for index, cameraID := range []string{"cam-1", "cam-2", "cam-3"} {
		if err := registry.ObserveCamera(contract.CameraObservation{
			CameraID: cameraID, HardwareID: "hw-" + cameraID, Endpoint: endpoints[index],
			Firmware: "1.0", Capabilities: []string{"person", "weapon"}, Online: true, LastSeen: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.ObserveCamera(contract.CameraObservation{
		CameraID: "cam-1", HardwareID: "hw-cam-1", Endpoint: "10.0.0.1",
		Firmware: "1.0", Capabilities: []string{"weapon", "person"}, Online: true, LastSeen: now,
	}); err != nil {
		t.Fatal(err)
	}
	if got := publisher.count(contract.EventDiscoveryCameraObserved); got != 3 {
		t.Fatalf("duplicate observation published %d events, want 3", got)
	}
	if err := registry.ObserveCamera(contract.CameraObservation{
		CameraID: "cam-1", HardwareID: "hw-cam-1", Endpoint: "10.0.0.9",
		Firmware: "1.0", Capabilities: []string{"person", "weapon"}, Online: true, LastSeen: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if got := publisher.count(contract.EventDiscoveryCameraObserved); got != 4 {
		t.Fatalf("endpoint change published %d events, want 4", got)
	}
	if err := registry.ObserveCamera(contract.CameraObservation{
		CameraID: "cam-other", HardwareID: "hw-cam-1", Endpoint: "10.0.0.20", Online: true, LastSeen: now,
	}); err == nil {
		t.Fatal("expected hardware identity collision")
	}
	var decoded contract.CameraObservation
	if err := json.Unmarshal(publisher.messages[3].Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Endpoint != "10.0.0.9" || decoded.ObservationID == "" {
		t.Fatalf("unexpected changed observation: %#v", decoded)
	}
}

func TestRegistryOfflineOnlineFlappingPublishesTransitionsOnce(t *testing.T) {
	publisher := &fakePublisher{}
	registry := NewRegistry(publisher)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	registry.TouchCameraClip("cam-1", now)
	if !registry.MarkCameraOffline("cam-1", now.Add(time.Minute)) {
		t.Fatal("first offline transition was not reported")
	}
	if registry.MarkCameraOffline("cam-1", now.Add(2*time.Minute)) {
		t.Fatal("repeated offline state was reported as a transition")
	}
	if !registry.TouchCameraClip("cam-1", now.Add(3*time.Minute)) {
		t.Fatal("online recovery was not reported")
	}
	if got := publisher.count(contract.EventDiscoveryCameraOffline); got != 1 {
		t.Fatalf("offline events=%d, want 1", got)
	}
	if got := publisher.count(contract.EventDiscoveryCameraOnline); got != 2 {
		t.Fatalf("online events=%d, want 2", got)
	}
}
