package vision

import "time"

type ClipJob struct {
	ID           string
	ActivationID string
	ClipIndex    int
	NodeID       string
	SequenceKey  string
	TrackID      string

	CameraID string

	Path string

	CreatedAt time.Time
}
