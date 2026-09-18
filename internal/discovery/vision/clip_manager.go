package vision

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"synora/pkg/contract"
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
	zone      string
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
	key := episodeKey(cameraID, nodeID, zone)
	m.mu.Lock()
	defer m.mu.Unlock()
	previous, ok := m.last[key]
	near := ok && startedAt.Sub(previous.endsAt) >= 0 && startedAt.Sub(previous.endsAt) <= m.continuityWindow
	episodeID := "episode-" + uuid.NewString()
	if near {
		episodeID = previous.episodeID
	}
	window := ClipWindow{ClipID: clipID, EpisodeID: episodeID, CameraID: cameraID, NodeID: nodeID, Zone: zone, TriggerReason: reason, StartedAt: startedAt, EndsAt: startedAt.Add(m.maxDuration)}
	// A worker-supplied track is intentionally not recorded here. Track IDs
	// become episode evidence only after ObserveSummary has accepted a typed,
	// validated summary.
	m.last[key] = episodeState{episodeID: episodeID, cameraID: cameraID, nodeID: nodeID, zone: zone, endsAt: window.EndsAt, trackIDs: map[string]struct{}{}}
	return window, nil
}

// ObserveSummary records a worker track only after the summary has passed the
// typed V1 contract. The manager is in-memory by design; a new manager after a
// restart has no prior evidence and therefore starts a safe new episode.
func (m *ClipManager) ObserveSummary(summary contract.VisionClipSummary) error {
	if m == nil {
		return fmt.Errorf("clip manager unavailable")
	}
	if err := summary.Validate(); err != nil {
		return fmt.Errorf("invalid vision summary: %w", err)
	}
	key := episodeKey(summary.CameraID, summary.Topology.NodeID, summary.Topology.Zone)
	m.mu.Lock()
	defer m.mu.Unlock()
	state, ok := m.last[key]
	if !ok || state.episodeID != summary.EpisodeID {
		return fmt.Errorf("summary does not belong to an active episode")
	}
	state.trackIDs[summary.Track.ID] = struct{}{}
	m.last[key] = state
	return nil
}

func episodeKey(cameraID, nodeID, zone string) string {
	return cameraID + "\x00" + nodeID + "\x00" + zone
}
