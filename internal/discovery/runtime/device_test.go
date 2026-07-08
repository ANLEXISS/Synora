package runtime

import (
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
