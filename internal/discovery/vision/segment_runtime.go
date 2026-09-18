package vision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"synora/pkg/contract"
)

var (
	ErrSegmentRuntimeDisabled = errors.New("vision segment runtime disabled")
	ErrSegmentDuplicateHash   = errors.New("vision segment index already admitted with a different hash")
	ErrSegmentRuntimeFinal    = errors.New("vision episode already finalized")
)

type SegmentRuntimeConfig struct {
	Enabled               bool
	MinSegmentSeconds     float64
	MaxSegmentSeconds     float64
	ReorderWindowSegments int
	EpisodeTTLSeconds     float64
	MaxActiveEpisodes     int
	StatePath             string
}

func DefaultSegmentRuntimeConfig() SegmentRuntimeConfig {
	return SegmentRuntimeConfig{
		Enabled: false, MinSegmentSeconds: .25, MaxSegmentSeconds: 2,
		ReorderWindowSegments: 4, EpisodeTTLSeconds: 45, MaxActiveEpisodes: 32,
	}
}

func (c SegmentRuntimeConfig) validate() error {
	if c.MinSegmentSeconds < .25 || c.MinSegmentSeconds > 2 || c.MaxSegmentSeconds < c.MinSegmentSeconds || c.MaxSegmentSeconds > 2 {
		return fmt.Errorf("invalid segment duration configuration")
	}
	if c.ReorderWindowSegments < 0 || c.ReorderWindowSegments > 64 || c.EpisodeTTLSeconds <= 0 || c.MaxActiveEpisodes <= 0 || c.MaxActiveEpisodes > 1024 {
		return fmt.Errorf("invalid segment runtime bounds")
	}
	return nil
}

type SegmentProcessor interface {
	ProcessSegment(context.Context, contract.VisionSegmentReadyV1) ([]Event, error)
}

type EpisodeContextReleaser interface {
	ReleaseEpisode(string)
}

type segmentContinuityResetContextKey struct{}

func withSegmentContinuityReset(ctx context.Context, reset bool) context.Context {
	return context.WithValue(ctx, segmentContinuityResetContextKey{}, reset)
}

func segmentContinuityReset(ctx context.Context) bool {
	reset, _ := ctx.Value(segmentContinuityResetContextKey{}).(bool)
	return reset
}

type SegmentRuntimeMetrics struct {
	SegmentsReceived     int     `json:"segments_received"`
	SegmentsProcessed    int     `json:"segments_processed"`
	SegmentsCoalesced    int     `json:"segments_coalesced"`
	SegmentsRejected     int     `json:"segments_rejected"`
	SegmentGaps          int     `json:"segment_gaps"`
	TriggerToCandidateMS float64 `json:"trigger_to_candidate_ms"`
	TriggerToConfirmedMS float64 `json:"trigger_to_confirmed_ms"`
	EpisodeWallLatencyMS float64 `json:"episode_wall_latency_ms"`
	ContinuityResets     int     `json:"continuity_resets"`
}

func (m SegmentRuntimeMetrics) Validate() error {
	if m.TriggerToCandidateMS < 0 || m.TriggerToConfirmedMS < 0 || m.TriggerToConfirmedMS < m.TriggerToCandidateMS {
		return fmt.Errorf("invalid segment trigger latency ordering")
	}
	return nil
}

type SegmentRuntimeResult struct {
	Events          []Event
	Duplicate       bool
	GapDetected     bool
	ContinuityReset bool
	Finalized       bool
	GapReason       string
	Metrics         SegmentRuntimeMetrics
}

type segmentEpisodeState struct {
	CameraID                string                                `json:"camera_id"`
	NodeID                  string                                `json:"node_id"`
	EpisodeID               string                                `json:"episode_id"`
	TopologyClass           string                                `json:"topology_class"`
	Trigger                 string                                `json:"trigger"`
	NextIndex               int                                   `json:"next_index"`
	NextObservationSequence int                                   `json:"next_observation_sequence"`
	Processed               map[int]string                        `json:"processed"`
	Pending                 map[int]contract.VisionSegmentReadyV1 `json:"pending"`
	Evidence                []map[string]any                      `json:"evidence"`
	LastSeen                time.Time                             `json:"last_seen"`
	StartedAt               time.Time                             `json:"started_at"`
	Finalized               bool                                  `json:"finalized"`
	ContinuityReset         bool                                  `json:"continuity_reset"`
	WorkerContinuityReset   bool                                  `json:"-"`
	Metrics                 SegmentRuntimeMetrics                 `json:"metrics"`
	LastSummary             map[string]any                        `json:"last_summary,omitempty"`
}

type segmentRuntimeDisk struct {
	Episodes map[string]*segmentEpisodeState `json:"episodes"`
}

// EpisodeRuntimeV1 is the central, camera-independent episode assembler.
type EpisodeRuntimeV1 struct {
	mu        sync.Mutex
	cfg       SegmentRuntimeConfig
	processor SegmentProcessor
	episodes  map[string]*segmentEpisodeState
}

func NewEpisodeRuntimeV1(cfg SegmentRuntimeConfig, processor SegmentProcessor) (*EpisodeRuntimeV1, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	runtime := &EpisodeRuntimeV1{cfg: cfg, processor: processor, episodes: map[string]*segmentEpisodeState{}}
	if cfg.StatePath != "" {
		if err := runtime.load(); err != nil {
			return nil, err
		}
	}
	return runtime, nil
}

func (r *EpisodeRuntimeV1) Ingest(ctx context.Context, segment contract.VisionSegmentReadyV1) (SegmentRuntimeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := SegmentRuntimeResult{}
	if !r.cfg.Enabled {
		return result, ErrSegmentRuntimeDisabled
	}
	if err := segment.Validate(); err != nil {
		result.Metrics.SegmentsRejected++
		return result, err
	}
	if r.processor == nil {
		return result, errors.New("vision segment processor unavailable")
	}
	state, err := r.episode(segment)
	if err != nil {
		result.Metrics.SegmentsRejected++
		return result, err
	}
	state.Metrics.SegmentsReceived++
	state.LastSeen = time.Now().UTC()
	if hash, ok := state.Processed[segment.SegmentIndex]; ok {
		if hash != segment.ContentSHA256 {
			state.Metrics.SegmentsRejected++
			_ = r.save()
			return result, ErrSegmentDuplicateHash
		}
		state.Metrics.SegmentsCoalesced++
		result.Duplicate = true
		result.Metrics = state.Metrics
		_ = r.save()
		return result, nil
	}
	if pending, ok := state.Pending[segment.SegmentIndex]; ok {
		if pending.ContentSHA256 != segment.ContentSHA256 {
			state.Metrics.SegmentsRejected++
			_ = r.save()
			return result, ErrSegmentDuplicateHash
		}
		state.Metrics.SegmentsCoalesced++
		result.Duplicate = true
		result.Metrics = state.Metrics
		_ = r.save()
		return result, nil
	}
	continuityResetPending := state.ContinuityReset
	if continuityResetPending {
		result.ContinuityReset = true
	}
	if state.Finalized {
		state.Metrics.SegmentsRejected++
		return result, ErrSegmentRuntimeFinal
	}
	if segment.SegmentIndex > state.NextIndex+r.cfg.ReorderWindowSegments {
		state.Metrics.SegmentGaps++
		result.GapDetected = true
		result.GapReason = "segment_gap_detected"
		state.Pending[segment.SegmentIndex] = segment
		_ = r.save()
		result.Metrics = state.Metrics
		return result, nil
	}
	state.Pending[segment.SegmentIndex] = segment
	finalProcessed := false
	continuityResetSent := false
	for {
		next, ok := state.Pending[state.NextIndex]
		if !ok {
			break
		}
		delete(state.Pending, state.NextIndex)
		resetForProcess := continuityResetPending && !continuityResetSent
		events, processErr := r.processOne(ctx, state, next, resetForProcess)
		if processErr != nil {
			state.Metrics.SegmentsRejected++
			return result, processErr
		}
		result.Events = append(result.Events, events...)
		state.Processed[next.SegmentIndex] = next.ContentSHA256
		state.NextIndex++
		state.Metrics.SegmentsProcessed++
		if resetForProcess {
			state.ContinuityReset = false
			state.Metrics.ContinuityResets++
			state.WorkerContinuityReset = false
			continuityResetSent = true
		}
		if state.WorkerContinuityReset && !resetForProcess {
			result.ContinuityReset = true
			state.Metrics.ContinuityResets++
			state.WorkerContinuityReset = false
		}
		if next.IsFinal {
			finalProcessed = true
		}
	}
	if finalProcessed && len(state.Pending) == 0 {
		if state.LastSummary != nil {
			result.Events = append(result.Events, Event{Type: contract.EventVisionClipSummaryV1, TrackID: "episode-final", Payload: clonePayload(state.LastSummary)})
		}
		state.Finalized = true
		result.Finalized = true
		if releaser, ok := r.processor.(EpisodeContextReleaser); ok {
			releaser.ReleaseEpisode(state.EpisodeID)
		}
	} else if segment.IsFinal && len(state.Pending) != 0 {
		state.Metrics.SegmentGaps++
		result.GapDetected = true
		result.GapReason = "segment_gap_detected"
	}
	state.Metrics.EpisodeWallLatencyMS = time.Since(state.StartedAt).Seconds() * 1000
	result.Metrics = state.Metrics
	if err := r.save(); err != nil {
		return result, err
	}
	return result, nil
}

func (r *EpisodeRuntimeV1) episode(segment contract.VisionSegmentReadyV1) (*segmentEpisodeState, error) {
	state := r.episodes[segment.EpisodeID]
	if state != nil {
		if state.CameraID != segment.CameraID || state.NodeID != segment.NodeID || state.TopologyClass != segment.TopologyClass {
			return nil, fmt.Errorf("episode context changed for %s", segment.EpisodeID)
		}
		return state, nil
	}
	if len(r.episodes) >= r.cfg.MaxActiveEpisodes {
		return nil, errors.New("active episode limit reached")
	}
	now := time.Now().UTC()
	state = &segmentEpisodeState{
		CameraID: segment.CameraID, NodeID: segment.NodeID, EpisodeID: segment.EpisodeID,
		TopologyClass: segment.TopologyClass, Trigger: segment.Trigger, Processed: map[int]string{}, Pending: map[int]contract.VisionSegmentReadyV1{},
		LastSeen: now, StartedAt: segment.StartedAt,
	}
	r.episodes[segment.EpisodeID] = state
	return state, nil
}

func (r *EpisodeRuntimeV1) processOne(ctx context.Context, state *segmentEpisodeState, segment contract.VisionSegmentReadyV1, continuityReset bool) ([]Event, error) {
	events, err := r.processor.ProcessSegment(withSegmentContinuityReset(ctx, continuityReset), segment)
	if err != nil {
		return nil, err
	}
	filtered := make([]Event, 0, len(events))
	for index := range events {
		event := &events[index]
		if event.Type == workerContinuityResetEvent {
			state.WorkerContinuityReset = true
			continue
		}
		if eventHasReason(event, "continuity_reset") {
			state.WorkerContinuityReset = true
		}
		if event.Type == contract.EventVisionClipObservationV1 {
			r.rewriteObservation(state, event.Payload, segment)
			state.Evidence = appendUniqueEvidence(state.Evidence, timelineFromPayload(event.Payload)...)
		} else if event.Type == contract.EventVisionClipSummaryV1 {
			state.LastSummary = clonePayload(event.Payload)
			state.LastSummary["episode_id"] = state.EpisodeID
			state.LastSummary["clip_id"] = state.EpisodeID + ":final"
			state.LastSummary["camera_id"] = state.CameraID
			state.LastSummary["topology"] = map[string]any{"node_id": state.NodeID, "zone": state.TopologyClass}
			state.LastSummary["topology_class"] = state.TopologyClass
			state.LastSummary["trigger"] = map[string]any{"reason": state.Trigger, "started_at": state.StartedAt}
			continue
		}
		filtered = append(filtered, *event)
	}
	return filtered, nil
}

func eventHasReason(event *Event, wanted string) bool {
	if event.Type != contract.EventVisionClipObservationV1 && event.Type != contract.EventVisionClipSummaryV1 {
		return false
	}
	if reasons, ok := event.Payload["reason_codes"].([]any); ok {
		for _, reason := range reasons {
			if value, ok := reason.(string); ok && value == wanted {
				return true
			}
		}
	}
	if reasonStrings, ok := event.Payload["reason_codes"].([]string); ok {
		for _, reason := range reasonStrings {
			if reason == wanted {
				return true
			}
		}
	}
	return false
}

func (r *EpisodeRuntimeV1) rewriteObservation(state *segmentEpisodeState, payload map[string]any, segment contract.VisionSegmentReadyV1) {
	payload["episode_id"] = state.EpisodeID
	payload["clip_id"] = state.EpisodeID
	payload["camera_id"] = state.CameraID
	payload["node_id"] = state.NodeID
	payload["zone"] = state.TopologyClass
	payload["trigger"] = state.Trigger
	payload["topology_class"] = state.TopologyClass
	if observedAt, ok := payload["observed_at"].(string); !ok || observedAt == "" {
		payload["observed_at"] = segment.EndedAt
	}
	observedAt := segment.EndedAt
	if rawObservedAt, ok := payload["observed_at"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339Nano, rawObservedAt); err == nil {
			observedAt = parsed
		}
	}
	if priorityState, _ := payload["priority_state"].(string); priorityState == "candidate" && state.Metrics.TriggerToCandidateMS == 0 {
		state.Metrics.TriggerToCandidateMS = maxDurationMS(observedAt.Sub(state.StartedAt))
	}
	if priorityState, _ := payload["priority_state"].(string); priorityState == "confirmed" && state.Metrics.TriggerToConfirmedMS == 0 {
		state.Metrics.TriggerToConfirmedMS = maxFloat(state.Metrics.TriggerToCandidateMS, maxDurationMS(observedAt.Sub(state.StartedAt)))
	}
	state.NextObservationSequence++
	payload["sequence"] = state.NextObservationSequence
}

func maxDurationMS(duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return duration.Seconds() * 1000
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func timelineFromPayload(payload map[string]any) []map[string]any {
	raw, _ := payload["priority_timeline"].([]any)
	result := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		if value, ok := entry.(map[string]any); ok {
			result = append(result, clonePayload(value))
		}
	}
	return result
}

func appendUniqueEvidence(existing []map[string]any, additions ...map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, item := range existing {
		encoded, _ := json.Marshal(item)
		seen[string(encoded)] = struct{}{}
	}
	for _, item := range additions {
		encoded, _ := json.Marshal(item)
		if _, ok := seen[string(encoded)]; ok {
			continue
		}
		seen[string(encoded)] = struct{}{}
		existing = append(existing, item)
	}
	return existing
}

func (r *EpisodeRuntimeV1) load() error {
	data, err := os.ReadFile(r.cfg.StatePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var disk segmentRuntimeDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		return err
	}
	if disk.Episodes != nil {
		r.episodes = disk.Episodes
	}
	for _, state := range r.episodes {
		if state.Processed == nil {
			state.Processed = map[int]string{}
		}
		if state.Pending == nil {
			state.Pending = map[int]contract.VisionSegmentReadyV1{}
		}
		if !state.Finalized {
			state.ContinuityReset = true
		}
	}
	return nil
}

// Expire closes episodes whose segment stream has gone quiet. The last
// summary is retained as an uncertain closure and an explicit gap is returned
// to the publisher so Core can distinguish a clean final segment from a loss.
func (r *EpisodeRuntimeV1) Expire(now time.Time) []SegmentRuntimeResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	results := make([]SegmentRuntimeResult, 0)
	for _, state := range r.episodes {
		if state.Finalized || now.Sub(state.LastSeen).Seconds() <= r.cfg.EpisodeTTLSeconds {
			continue
		}
		state.Metrics.SegmentGaps++
		state.Finalized = true
		result := SegmentRuntimeResult{GapDetected: true, Finalized: true, GapReason: "segment_gap_detected", Metrics: state.Metrics}
		if state.LastSummary != nil {
			result.Events = []Event{{Type: contract.EventVisionClipSummaryV1, TrackID: "episode-final", Payload: clonePayload(state.LastSummary)}}
		}
		results = append(results, result)
		if releaser, ok := r.processor.(EpisodeContextReleaser); ok {
			releaser.ReleaseEpisode(state.EpisodeID)
		}
	}
	_ = r.save()
	return results
}

func (r *EpisodeRuntimeV1) save() error {
	if r.cfg.StatePath == "" {
		return nil
	}
	data, err := json.Marshal(segmentRuntimeDisk{Episodes: r.episodes})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.cfg.StatePath), 0700); err != nil {
		return err
	}
	tmp := r.cfg.StatePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, r.cfg.StatePath)
}
