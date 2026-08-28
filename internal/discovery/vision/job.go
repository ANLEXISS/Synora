package vision

import "time"

type ClipJob struct {
	ID           string `json:"id"`
	ActivationID string `json:"activation_id,omitempty"`
	ClipIndex    int    `json:"clip_index,omitempty"`
	NodeID       string `json:"node_id,omitempty"`
	SequenceKey  string `json:"sequence_key,omitempty"`
	TrackID      string `json:"track_id,omitempty"`

	CameraID string `json:"camera_id"`

	Path string `json:"path"`

	CreatedAt time.Time `json:"created_at"`
}
