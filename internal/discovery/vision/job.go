package vision

import (
	"time"

	"synora/pkg/contract"
)

func authoritativeTopologyClass(job *ClipJob) string {
	if job != nil && contract.ValidVisionTopologyClass(job.TopologyClass) {
		return job.TopologyClass
	}
	if job != nil && contract.ValidVisionTopologyClass(job.Zone) {
		return job.Zone
	}
	return contract.VisionTopologyUnknown
}

type ClipJob struct {
	ID            string    `json:"id"`
	ActivationID  string    `json:"activation_id,omitempty"`
	ClipIndex     int       `json:"clip_index,omitempty"`
	NodeID        string    `json:"node_id,omitempty"`
	SequenceKey   string    `json:"sequence_key,omitempty"`
	TrackID       string    `json:"track_id,omitempty"`
	EpisodeID     string    `json:"episode_id,omitempty"`
	Zone          string    `json:"zone,omitempty"`
	TopologyClass string    `json:"topology_class,omitempty"`
	TriggerReason string    `json:"trigger_reason,omitempty"`
	StartedAt     time.Time `json:"started_at,omitempty"`
	EndsAt        time.Time `json:"ends_at,omitempty"`
	Pipeline      string    `json:"pipeline,omitempty"`

	CameraID string `json:"camera_id"`

	Path string `json:"path"`

	CreatedAt time.Time `json:"created_at"`
}
