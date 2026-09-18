package vision

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"synora/pkg/contract"
)

// PublishSegmentRuntimeResult is the only Discovery-to-Core bridge for the
// finalized-segment path. It publishes metadata and typed derived events, but
// never media bytes or raw detector output.
func PublishSegmentRuntimeResult(publisher Publisher, segment contract.VisionSegmentReadyV1, result SegmentRuntimeResult) error {
	if publisher == nil {
		return errors.New("segment publisher unavailable")
	}
	if err := segment.Validate(); err != nil {
		return err
	}
	if result.Duplicate {
		return nil
	}
	segmentPayload, err := segment.MarshalPayload()
	if err != nil {
		return err
	}
	if err := publisher.Send(contract.Message{
		ID: segment.SegmentID + ":ready", Type: contract.EventVisionSegmentReadyV1,
		Kind: contract.KindEvent, Source: "discovery", Target: "core", Timestamp: time.Now().UTC(),
		Priority: contract.PriorityNormal,
		Payload:  segmentPayload,
	}); err != nil {
		return err
	}
	if result.GapDetected {
		gapPayload, _ := json.Marshal(map[string]any{
			"schema_version":   contract.EventVisionSegmentGapV1,
			"episode_id":       segment.EpisodeID,
			"camera_id":        segment.CameraID,
			"segment_index":    segment.SegmentIndex,
			"reason":           result.GapReason,
			"continuity_reset": result.ContinuityReset,
		})
		if err := publisher.Send(contract.Message{
			ID: segment.SegmentID + ":gap", Type: contract.EventVisionSegmentGapV1,
			Kind: contract.KindEvent, Source: "discovery", Target: "core", Timestamp: time.Now().UTC(),
			Priority: contract.PriorityHigh,
			Payload:  gapPayload,
		}); err != nil {
			return err
		}
	}
	for index, event := range result.Events {
		payload := clonePayload(event.Payload)
		stableID := fmt.Sprintf("%s:event:%d:%s", segment.SegmentID, index, event.Type)
		payload["event_id"] = stableID
		payload["device_id"] = segment.CameraID
		payload["camera_id"] = segment.CameraID
		payload["node_id"] = segment.NodeID
		if event.Type == contract.EventVisionClipObservationV1 {
			if _, err := contract.DecodeVisionClipObservation(mustJSON(payload)); err != nil {
				return fmt.Errorf("segment observation contract invalid: %w", err)
			}
		} else if event.Type == contract.EventVisionClipSummaryV1 {
			if _, err := contract.DecodeVisionClipSummary(mustJSON(payload)); err != nil {
				return fmt.Errorf("segment summary contract invalid: %w", err)
			}
		} else {
			return fmt.Errorf("segment event type %q is not admitted", event.Type)
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if err := publisher.Send(contract.Message{
			ID: stableID, Type: event.Type, Kind: contract.KindEvent, Source: "discovery", Target: "core",
			Timestamp: time.Now().UTC(), Priority: visionEventPriority(event.Type, payload),
			TrackID: fmt.Sprint(event.TrackID), Payload: encoded,
		}); err != nil {
			return err
		}
	}
	return nil
}
