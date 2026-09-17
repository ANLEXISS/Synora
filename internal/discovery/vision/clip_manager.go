package vision

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ClipWindow is the lifecycle metadata assigned before an uploaded V1 clip
// enters analysis. It is deliberately independent from recording hardware.
type ClipWindow struct {
	ClipID        string
	EpisodeID     string
	CameraID      string
	NodeID        string
	Zone          string
	TriggerReason string
	StartedAt     time.Time
	EndsAt        time.Time
}

type episodeState struct {
	episodeID string
	cameraID  string
	nodeID    string
	endsAt    time.Time
	trackIDs  map[string]struct{}
}

// ClipManager assigns a stable episode to near-consecutive clips. It never
// invokes a recorder, device command or action surface.
type ClipManager struct {
	mu               sync.Mutex
	maxDuration      time.Duration
	continuityWindow time.Duration
	last             map[string]episodeState
	now              func() time.Time
}

func NewClipManager(maxDuration, continuityWindow time.Duration) (*ClipManager, error) {
	if maxDuration <= 0 || continuityWindow < 0 {
		return nil, fmt.Errorf("invalid clip manager duration configuration")
	}
	return &ClipManager{maxDuration: maxDuration, continuityWindow: continuityWindow, last: map[string]episodeState{}, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (m *ClipManager) Open(clipID, cameraID, nodeID, zone, reason, trackID string, startedAt time.Time) (ClipWindow, error) {
	if m == nil || cameraID == "" || clipID == "" {
		return ClipWindow{}, fmt.Errorf("clip id and camera id are required")
	}
	if startedAt.IsZero() {
		startedAt = m.now()
	}
	startedAt = startedAt.UTC()
	key := cameraID + "\x00" + nodeID
	m.mu.Lock()
	defer m.mu.Unlock()
	previous, ok := m.last[key]
	_, sameTrack := previous.trackIDs[trackID]
	near := ok && startedAt.Sub(previous.endsAt) >= 0 && startedAt.Sub(previous.endsAt) <= m.continuityWindow
	episodeID := "episode-" + uuid.NewString()
	if ok && (near || sameTrack) {
		episodeID = previous.episodeID
	}
	window := ClipWindow{ClipID: clipID, EpisodeID: episodeID, CameraID: cameraID, NodeID: nodeID, Zone: zone, TriggerReason: reason, StartedAt: startedAt, EndsAt: startedAt.Add(m.maxDuration)}
	tracks := map[string]struct{}{}
	if trackID != "" {
		tracks[trackID] = struct{}{}
	}
	m.last[key] = episodeState{episodeID: episodeID, cameraID: cameraID, nodeID: nodeID, endsAt: window.EndsAt, trackIDs: tracks}
	return window, nil
}
