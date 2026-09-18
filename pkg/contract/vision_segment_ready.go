package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// VisionSegmentReadyV1 is emitted only after a segment file is closed and its
// content hash has been computed. The bus carries a local reference, never
// media bytes or derived sensitive data.
type VisionSegmentReadyV1 struct {
	SchemaVersion string    `json:"schema_version"`
	CameraID      string    `json:"camera_id"`
	NodeID        string    `json:"node_id"`
	EpisodeID     string    `json:"episode_id"`
	SegmentID     string    `json:"segment_id"`
	SegmentIndex  int       `json:"segment_index"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	IsFinal       bool      `json:"is_final"`
	TopologyClass string    `json:"topology_class"`
	Trigger       string    `json:"trigger"`
	MediaRef      string    `json:"media_ref"`
	ContentSHA256 string    `json:"content_sha256"`
}

// VisionSegmentReady is kept as a short internal-facing alias while the
// versioned wire name remains explicit at every boundary.
type VisionSegmentReady = VisionSegmentReadyV1

func (s VisionSegmentReadyV1) Validate() error {
	if s.SchemaVersion != EventVisionSegmentReadyV1 || !validScalar(s.CameraID) || !validScalar(s.NodeID) || !validScalar(s.EpisodeID) || !validScalar(s.SegmentID) || s.SegmentIndex < 0 || s.SegmentIndex > 1000000 {
		return fmt.Errorf("invalid vision segment identity")
	}
	if s.StartedAt.IsZero() || s.EndedAt.IsZero() || !s.EndedAt.After(s.StartedAt) {
		return fmt.Errorf("invalid vision segment timestamps")
	}
	duration := s.EndedAt.Sub(s.StartedAt)
	if duration < 250*time.Millisecond || duration > 2*time.Second {
		return fmt.Errorf("vision segment duration outside 250ms..2s")
	}
	if !ValidVisionTopologyClass(s.TopologyClass) || !validScalar(s.Trigger) {
		return fmt.Errorf("invalid vision segment context")
	}
	if !validLocalReference(s.MediaRef) || !strings.HasPrefix(s.MediaRef, "local://") {
		return fmt.Errorf("vision segment media reference must be local")
	}
	if len(s.ContentSHA256) != sha256.Size*2 {
		return fmt.Errorf("invalid vision segment content hash")
	}
	if _, err := hex.DecodeString(s.ContentSHA256); err != nil {
		return fmt.Errorf("invalid vision segment content hash")
	}
	if expected := DeterministicVisionSegmentID(s.CameraID, s.EpisodeID, s.SegmentIndex, s.ContentSHA256); s.SegmentID != expected {
		return fmt.Errorf("vision segment id is not deterministic")
	}
	return nil
}

func DeterministicVisionSegmentID(cameraID, episodeID string, segmentIndex int, contentSHA256 string) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d|%s", cameraID, episodeID, segmentIndex, strings.ToLower(contentSHA256))))
	return "segment-" + hex.EncodeToString(digest[:])[:24]
}

func DecodeVisionSegmentReadyV1(data []byte) (VisionSegmentReadyV1, error) {
	var segment VisionSegmentReadyV1
	if err := decodeTypedPayload(data, map[string]struct{}{
		"schema_version": {}, "camera_id": {}, "node_id": {}, "episode_id": {}, "segment_id": {},
		"segment_index": {}, "started_at": {}, "ended_at": {}, "is_final": {}, "topology_class": {},
		"trigger": {}, "media_ref": {}, "content_sha256": {},
		"device_id": {}, "event_id": {}, "activation_id": {}, "sequence_key": {}, "clip_index": {},
	}, &segment); err != nil {
		return VisionSegmentReadyV1{}, err
	}
	if err := segment.Validate(); err != nil {
		return VisionSegmentReadyV1{}, err
	}
	return segment, nil
}

func (s VisionSegmentReadyV1) MarshalPayload() ([]byte, error) {
	return json.Marshal(s)
}
