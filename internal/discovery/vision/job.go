package vision

import "time"

type ClipJob struct {
	ID string

	CameraID string

	Path string

	CreatedAt time.Time
}
