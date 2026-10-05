package discovery

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"synora/pkg/contract"
)

const EdgeTrackManifestSchemaV1 = "synora.vision.edge-track-manifest/v1"

var edgeIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// EdgeTrackManifestV1 is the semantic handoff emitted by the local Edge
// tracker. Discovery admits it without accepting pixel-space state.
type EdgeTrackManifestV1 struct {
	SchemaVersion       string         `json:"schema_version"`
	CameraID            string         `json:"camera_id"`
	EpisodeID           string         `json:"episode_id"`
	TopologyClass       string         `json:"topology_class"`
	TriggerClass        string         `json:"trigger_class"`
	TriggerConfidence   float64        `json:"trigger_confidence"`
	TrackingStatus      string         `json:"tracking_status"`
	StartedAt           string         `json:"started_at"`
	EndedAt             string         `json:"ended_at"`
	TrackCount          int            `json:"track_count"`
	ConfirmedTrackCount int            `json:"confirmed_track_count"`
	ObservationCount    int            `json:"observation_count"`
	SegmentCount        int            `json:"segment_count"`
	GapCount            int            `json:"gap_count"`
	EvidenceRefs        []string       `json:"evidence_refs"`
	PriorityReason      []string       `json:"priority_reason"`
	EdgeEmulated        bool           `json:"edge_emulated"`
	Metrics             map[string]any `json:"metrics"`
}

// AcceptEdgeTrackManifest is the Discovery-side admission point for the
// Edge manifest. It is intentionally independent of HTTP and does not expose
// a production route for a test-only bus producer.
func AcceptEdgeTrackManifest(data []byte) (EdgeTrackManifestV1, error) {
	return AcceptEdgeTrackManifestAt(data, time.Now().UTC())
}

// AcceptEdgeTrackManifestAt is the deterministic admission variant used by
// the central harness. Production ingress uses the wall-clock wrapper above.
func AcceptEdgeTrackManifestAt(data []byte, now time.Time) (EdgeTrackManifestV1, error) {
	if len(data) == 0 {
		return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest is empty")
	}
	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawFields); err != nil {
		return EdgeTrackManifestV1{}, fmt.Errorf("decode edge manifest: %w", err)
	}
	if _, ok := rawFields["trigger_confidence"]; !ok {
		return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest trigger_confidence is required")
	}
	if IsForbiddenValue(mustDecodeJSON(data)) {
		return EdgeTrackManifestV1{}, ErrForbiddenBoundaryData
	}
	var manifest EdgeTrackManifestV1
	if err := json.Unmarshal(data, &manifest); err != nil {
		return EdgeTrackManifestV1{}, fmt.Errorf("decode edge manifest: %w", err)
	}
	if manifest.SchemaVersion != EdgeTrackManifestSchemaV1 {
		return EdgeTrackManifestV1{}, fmt.Errorf("unsupported edge manifest schema %q", manifest.SchemaVersion)
	}
	for name, value := range map[string]string{
		"camera_id": manifest.CameraID, "episode_id": manifest.EpisodeID,
		"topology_class": manifest.TopologyClass, "trigger_class": manifest.TriggerClass,
		"tracking_status": manifest.TrackingStatus, "started_at": manifest.StartedAt,
		"ended_at": manifest.EndedAt,
	} {
		if strings.TrimSpace(value) == "" {
			return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest %s is required", name)
		}
		if !edgeIdentifierPattern.MatchString(value) && name != "started_at" && name != "ended_at" {
			return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest %s is invalid", name)
		}
	}
	startedAt, err := time.Parse(time.RFC3339, manifest.StartedAt)
	if err != nil {
		return EdgeTrackManifestV1{}, fmt.Errorf("invalid edge started_at")
	}
	endedAt, err := time.Parse(time.RFC3339, manifest.EndedAt)
	if err != nil || endedAt.Before(startedAt) {
		return EdgeTrackManifestV1{}, fmt.Errorf("invalid edge ended_at")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.Sub(startedAt) > 10*time.Minute || startedAt.Sub(now) > 10*time.Minute || now.Sub(endedAt) > 10*time.Minute || endedAt.Sub(now) > 10*time.Minute {
		return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest timestamp outside admission window")
	}
	if !contract.ValidVisionTopologyClass(manifest.TopologyClass) {
		return EdgeTrackManifestV1{}, fmt.Errorf("invalid edge topology %q", manifest.TopologyClass)
	}
	if manifest.TriggerClass != "human" && manifest.TriggerClass != "vehicle" && manifest.TriggerClass != "animal" && manifest.TriggerClass != "unknown" {
		return EdgeTrackManifestV1{}, fmt.Errorf("invalid edge trigger class %q", manifest.TriggerClass)
	}
	if manifest.TrackingStatus != "ok" && manifest.TrackingStatus != "unavailable" && manifest.TrackingStatus != "invalid" && manifest.TrackingStatus != "contradictory" {
		return EdgeTrackManifestV1{}, fmt.Errorf("invalid edge tracking status %q", manifest.TrackingStatus)
	}
	if manifest.TriggerConfidence < 0 || manifest.TriggerConfidence > 1 {
		return EdgeTrackManifestV1{}, fmt.Errorf("edge trigger confidence is outside 0..1")
	}
	for name, value := range map[string]int{
		"track_count": manifest.TrackCount, "confirmed_track_count": manifest.ConfirmedTrackCount,
		"observation_count": manifest.ObservationCount, "segment_count": manifest.SegmentCount,
		"gap_count": manifest.GapCount,
	} {
		if value < 0 {
			return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest %s must be non-negative", name)
		}
	}
	if len(manifest.EvidenceRefs) > 1024 || len(manifest.PriorityReason) > 1024 {
		return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest lists exceed bounds")
	}
	if manifest.Metrics == nil {
		return EdgeTrackManifestV1{}, fmt.Errorf("edge manifest metrics are required")
	}
	return manifest, nil
}

func mustDecodeJSON(data []byte) any {
	var value any
	if json.Unmarshal(data, &value) != nil {
		return map[string]any{"invalid_json": true}
	}
	return value
}
