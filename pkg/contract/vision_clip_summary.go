package contract

import (
	"fmt"
	"strings"
	"time"
)

type VisionClipTopology struct {
	NodeID string `json:"node_id"`
	Zone   string `json:"zone"`
}

type VisionClipTrigger struct {
	Reason    string    `json:"reason"`
	StartedAt time.Time `json:"started_at"`
}

type VisionClipTrack struct {
	ID          string    `json:"id"`
	SubjectType string    `json:"subject_type"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`
	Confidence  float64   `json:"confidence"`
}

type VisionClipIdentity struct {
	Status       string  `json:"status"`
	Confidence   float64 `json:"confidence"`
	EmbeddingRef *string `json:"embedding_ref"`
}

type VisionClipPlate struct {
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	ValueRef   *string `json:"value_ref"`
}

type VisionSensitiveDetection struct {
	Kind       string  `json:"kind,omitempty"`
	Confidence float64 `json:"confidence"`
	ROIRef     *string `json:"roi_ref,omitempty"`
}

type VisionSensitiveObjects struct {
	Status     string                     `json:"status"`
	Detections []VisionSensitiveDetection `json:"detections"`
}

type VisionClipMedia struct {
	ClipRef     *string  `json:"clip_ref"`
	BestROIRefs []string `json:"best_roi_refs"`
}

type VisionClipSummary struct {
	Schema    string                 `json:"schema"`
	EpisodeID string                 `json:"episode_id"`
	ClipID    string                 `json:"clip_id"`
	CameraID  string                 `json:"camera_id"`
	Topology  VisionClipTopology     `json:"topology"`
	Trigger   VisionClipTrigger      `json:"trigger"`
	Track     VisionClipTrack        `json:"track"`
	Identity  VisionClipIdentity     `json:"identity"`
	Plate     VisionClipPlate        `json:"plate"`
	Sensitive VisionSensitiveObjects `json:"sensitive_objects"`
	Media     VisionClipMedia        `json:"media"`
}

func (s VisionClipSummary) Validate() error {
	if s.Schema != EventVisionClipSummaryV1 || s.EpisodeID == "" || s.ClipID == "" || s.CameraID == "" {
		return fmt.Errorf("invalid vision clip summary identity")
	}
	if s.Topology.NodeID == "" || s.Topology.Zone == "" || s.Trigger.Reason == "" || s.Trigger.StartedAt.IsZero() {
		return fmt.Errorf("invalid vision clip summary context")
	}
	if s.Track.ID == "" || !validVisionSubjectType(s.Track.SubjectType) || s.Track.FirstSeenAt.IsZero() || s.Track.LastSeenAt.IsZero() {
		return fmt.Errorf("invalid vision clip summary track")
	}
	if !validConfidence(s.Track.Confidence) || !validConfidence(s.Identity.Confidence) || !validConfidence(s.Plate.Confidence) {
		return fmt.Errorf("invalid vision clip summary confidence")
	}
	if !validIdentityStatus(s.Identity.Status) || !validIdentityStatus(s.Plate.Status) {
		return fmt.Errorf("invalid vision clip summary enrichment status")
	}
	if !validSensitiveStatus(s.Sensitive.Status) {
		return fmt.Errorf("invalid vision clip summary sensitive status")
	}
	for _, ref := range s.Media.BestROIRefs {
		if !validLocalReference(ref) {
			return fmt.Errorf("invalid vision clip ROI reference")
		}
	}
	if s.Media.ClipRef != nil && !validLocalReference(*s.Media.ClipRef) {
		return fmt.Errorf("invalid vision clip reference")
	}
	if s.Identity.EmbeddingRef != nil && !validLocalReference(*s.Identity.EmbeddingRef) {
		return fmt.Errorf("invalid embedding reference")
	}
	if s.Plate.ValueRef != nil && !validLocalReference(*s.Plate.ValueRef) {
		return fmt.Errorf("invalid plate reference")
	}
	return nil
}

func validVisionSubjectType(value string) bool {
	switch value {
	case "human", "animal", "vehicle", "unknown":
		return true
	}
	return false
}

func validIdentityStatus(value string) bool {
	switch value {
	case "recognized", "uncertain", "unknown", "not_available":
		return true
	}
	return false
}

func validSensitiveStatus(value string) bool {
	switch value {
	case "clear", "detected", "inconclusive", "not_available":
		return true
	}
	return false
}

func validConfidence(value float64) bool { return value >= 0 && value <= 1 }

func validLocalReference(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "local://") || strings.HasPrefix(value, "/var/lib/synora/")
}
