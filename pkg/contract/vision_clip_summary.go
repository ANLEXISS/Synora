package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
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

type VisionClipBackendDiagnostic struct {
	Name            string  `json:"name"`
	ModelVersion    string  `json:"model_version"`
	RealModel       bool    `json:"real_model"`
	Status          string  `json:"status"`
	FramesSampled   int     `json:"frames_sampled"`
	DetectionsTotal int     `json:"detections_total"`
	LatencyMS       float64 `json:"latency_ms"`
	NonHumanIgnored int     `json:"non_human_ignored"`
	ErrorCode       string  `json:"error_code,omitempty"`
}

type VisionPreliminaryTrack struct {
	ID          string `json:"id"`
	SubjectType string `json:"subject_type"`
}

type VisionPreliminaryAlertBody struct {
	Kind       string                     `json:"kind"`
	Confidence float64                    `json:"confidence"`
	Detections []VisionSensitiveDetection `json:"detections"`
}

type VisionPreliminaryAlert struct {
	Schema    string                     `json:"schema"`
	EpisodeID string                     `json:"episode_id"`
	ClipID    string                     `json:"clip_id"`
	CameraID  string                     `json:"camera_id"`
	Topology  VisionClipTopology         `json:"topology"`
	Track     VisionPreliminaryTrack     `json:"track"`
	Alert     VisionPreliminaryAlertBody `json:"alert"`
}

type VisionClipSummary struct {
	Schema    string                      `json:"schema"`
	EpisodeID string                      `json:"episode_id"`
	ClipID    string                      `json:"clip_id"`
	CameraID  string                      `json:"camera_id"`
	Topology  VisionClipTopology          `json:"topology"`
	Trigger   VisionClipTrigger           `json:"trigger"`
	Track     VisionClipTrack             `json:"track"`
	Identity  VisionClipIdentity          `json:"identity"`
	Plate     VisionClipPlate             `json:"plate"`
	Sensitive VisionSensitiveObjects      `json:"sensitive_objects"`
	Media     VisionClipMedia             `json:"media"`
	Backend   VisionClipBackendDiagnostic `json:"backend"`
}

func (s VisionClipSummary) Validate() error {
	if s.Schema != EventVisionClipSummaryV1 || !validScalar(s.EpisodeID) || !validScalar(s.ClipID) || !validScalar(s.CameraID) {
		return fmt.Errorf("invalid vision clip summary identity")
	}
	if !validScalar(s.Topology.NodeID) || !validScalar(s.Topology.Zone) || !validScalar(s.Trigger.Reason) || s.Trigger.StartedAt.IsZero() {
		return fmt.Errorf("invalid vision clip summary context")
	}
	if !validScalar(s.Track.ID) || !validVisionSubjectType(s.Track.SubjectType) || s.Track.FirstSeenAt.IsZero() || s.Track.LastSeenAt.IsZero() || s.Track.LastSeenAt.Before(s.Track.FirstSeenAt) || s.Track.FirstSeenAt.Before(s.Trigger.StartedAt) {
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
	if err := s.Backend.Validate(); err != nil {
		return err
	}
	if s.Identity.Status == "recognized" && s.Identity.EmbeddingRef == nil {
		return fmt.Errorf("recognized identity requires an embedding reference")
	}
	if s.Plate.Status == "recognized" && s.Plate.ValueRef == nil {
		return fmt.Errorf("recognized plate requires a value reference")
	}
	if err := validateSensitiveDetections(s.Sensitive.Detections); err != nil {
		return err
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

func (d VisionClipBackendDiagnostic) Validate() error {
	if !validScalar(d.Name) || !validScalar(d.ModelVersion) {
		return fmt.Errorf("invalid vision backend identity")
	}
	switch d.Status {
	case "ok", "unavailable", "failed", "timeout":
	default:
		return fmt.Errorf("invalid vision backend status")
	}
	if d.FramesSampled < 0 || d.FramesSampled > 1000000 || d.DetectionsTotal < 0 || d.DetectionsTotal > 10000000 || d.NonHumanIgnored < 0 || d.NonHumanIgnored > 10000000 {
		return fmt.Errorf("invalid vision backend counters")
	}
	if math.IsNaN(d.LatencyMS) || math.IsInf(d.LatencyMS, 0) || d.LatencyMS < 0 || d.LatencyMS > 3600000 {
		return fmt.Errorf("invalid vision backend latency")
	}
	if d.Status == "ok" && !d.RealModel {
		return fmt.Errorf("ok vision backend must have a real model")
	}
	if d.Status == "unavailable" && d.RealModel {
		return fmt.Errorf("unavailable vision backend cannot have a real model")
	}
	if d.Status != "ok" && d.ErrorCode != "" && !validScalar(d.ErrorCode) {
		return fmt.Errorf("invalid vision backend error code")
	}
	return nil
}

func (s VisionPreliminaryAlert) Validate() error {
	if s.Schema != EventVisionPreliminaryAlertV1 || !validScalar(s.EpisodeID) || !validScalar(s.ClipID) || !validScalar(s.CameraID) {
		return fmt.Errorf("invalid preliminary alert identity")
	}
	if !validScalar(s.Topology.NodeID) || !validScalar(s.Topology.Zone) || !validScalar(s.Track.ID) || !validVisionSubjectType(s.Track.SubjectType) {
		return fmt.Errorf("invalid preliminary alert context")
	}
	if !validScalar(s.Alert.Kind) || !validConfidence(s.Alert.Confidence) || len(s.Alert.Detections) == 0 {
		return fmt.Errorf("invalid preliminary alert body")
	}
	return validateSensitiveDetections(s.Alert.Detections)
}

// DecodeVisionClipSummary accepts only the transport metadata added by
// Discovery plus the typed summary fields. DisallowUnknownFields protects the
// bus boundary from raw image/embedding/plate fields hidden in extensions.
func DecodeVisionClipSummary(data []byte) (VisionClipSummary, error) {
	var summary VisionClipSummary
	if err := decodeTypedPayload(data, map[string]struct{}{
		"schema": {}, "episode_id": {}, "clip_id": {}, "camera_id": {}, "topology": {}, "trigger": {},
		"track": {}, "identity": {}, "plate": {}, "sensitive_objects": {}, "media": {},
		"backend":   {},
		"device_id": {}, "node_id": {}, "track_id": {}, "event_id": {}, "activation_id": {}, "sequence_key": {}, "clip_index": {},
	}, &summary); err != nil {
		return VisionClipSummary{}, err
	}
	if err := summary.Validate(); err != nil {
		return VisionClipSummary{}, err
	}
	return summary, nil
}

func DecodeVisionPreliminaryAlert(data []byte) (VisionPreliminaryAlert, error) {
	var alert VisionPreliminaryAlert
	if err := decodeTypedPayload(data, map[string]struct{}{
		"schema": {}, "episode_id": {}, "clip_id": {}, "camera_id": {}, "topology": {}, "track": {}, "alert": {},
		"device_id": {}, "node_id": {}, "track_id": {}, "event_id": {},
	}, &alert); err != nil {
		return VisionPreliminaryAlert{}, err
	}
	if err := alert.Validate(); err != nil {
		return VisionPreliminaryAlert{}, err
	}
	return alert, nil
}

func decodeTypedPayload(data []byte, allowed map[string]struct{}, target any) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return fmt.Errorf("vision payload must be a JSON object")
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return fmt.Errorf("unknown vision payload field %q", key)
		}
	}
	clean := make(map[string]json.RawMessage, len(fields))
	for key, value := range fields {
		switch key {
		case "device_id", "node_id", "track_id", "event_id", "activation_id", "sequence_key", "clip_index":
			continue
		default:
			clean[key] = value
		}
	}
	encoded, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid typed vision payload: %w", err)
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

func validConfidence(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func validScalar(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\r\n")
}

func validateSensitiveDetections(detections []VisionSensitiveDetection) error {
	for _, detection := range detections {
		if !validScalar(detection.Kind) || !validConfidence(detection.Confidence) {
			return fmt.Errorf("invalid sensitive detection")
		}
		if detection.ROIRef != nil && !validLocalReference(*detection.ROIRef) {
			return fmt.Errorf("invalid sensitive ROI reference")
		}
	}
	return nil
}

func validLocalReference(value string) bool {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "local://") || len(value) > 256 {
		return false
	}
	value = strings.TrimPrefix(value, "local://")
	if value == "" || strings.Contains(value, "..") || strings.ContainsAny(value, "\r\n\t \"'") {
		return false
	}
	return !strings.Contains(value, "//")
}
