package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"synora/internal/cognitive"
	"synora/pkg/contract"
)

type stateFrameV5Manifest struct {
	Segments []struct {
		Segment contract.VisionSegmentReadyV1 `json:"segment"`
	} `json:"segments"`
}

func readStateFrameV5Segments(path string) ([]contract.VisionSegmentReadyV1, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read segment manifest: %w", err)
	}
	var manifest stateFrameV5Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("decode segment manifest: %w", err)
	}
	segments := make([]contract.VisionSegmentReadyV1, 0, len(manifest.Segments))
	for _, item := range manifest.Segments {
		if err := item.Segment.Validate(); err != nil {
			return nil, fmt.Errorf("invalid segment manifest entry: %w", err)
		}
		segments = append(segments, item.Segment)
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].SegmentIndex < segments[j].SegmentIndex })
	return segments, nil
}

func stateFrameV5For(input transitionInput, v4Frame cognitive.StateFrame, observations []visionObservation, segments []contract.VisionSegmentReadyV1) cognitive.StateFrameV5 {
	eligible := make([]visionObservation, 0, len(observations))
	for _, observation := range observations {
		if input.Name == transitionFinal || !observation.ObservedAt.After(input.At) {
			eligible = append(eligible, observation)
		}
	}
	latestByTrack := map[string]contract.VisionClipObservationTrack{}
	seenByTrack := map[string]int{}
	var latest *visionObservation
	for index := range eligible {
		observation := &eligible[index]
		if latest == nil || observation.ObservedAt.After(latest.ObservedAt) {
			latest = observation
		}
		for _, track := range observation.Tracks {
			seenByTrack[track.ID]++
			latestByTrack[track.ID] = track
		}
	}

	humanPresent := false
	trackConfirmed := false
	confidenceSum := float32(0)
	confidenceCount := 0
	for trackID, track := range latestByTrack {
		if track.SubjectType == "human" {
			humanPresent = true
			confidenceSum += float32(track.Confidence)
			confidenceCount++
		}
		if seenByTrack[trackID] >= 2 || track.State == "recognized_stable" {
			trackConfirmed = true
		}
	}
	aggregateConfidence := float32(0)
	if confidenceCount > 0 {
		aggregateConfidence = confidenceSum / float32(confidenceCount)
	}

	topology := input.Topology
	priority := input.Priority
	trigger := input.Trigger
	enrichment := cognitive.V5EnrichmentNotRequested
	realDetection := false
	if latest != nil {
		topology = latest.TopologyClass
		if priority == "" {
			priority = latest.PriorityHint
		}
		trigger = latest.Trigger
		realDetection = latest.Backend.Status == "ok" && latest.Backend.RealModel
		enrichment = stateFrameV5Enrichment(latest, len(latestByTrack))
	}
	if len(eligible) > 0 && latest != nil && latest.Backend.Status != "ok" {
		enrichment = cognitive.V5EnrichmentUnavailable
	}
	phase := stateFrameV5Phase(input.Name)
	segmentCount := stateFrameV5SegmentCount(input, segments)
	gapCount := stateFrameV5GapCount(eligible, segments)
	firstAt, lastAt := time.Time{}, time.Time{}
	if len(eligible) > 0 {
		firstAt = eligible[0].ObservedAt
		lastAt = eligible[0].ObservedAt
		for _, observation := range eligible[1:] {
			if observation.ObservedAt.Before(firstAt) {
				firstAt = observation.ObservedAt
			}
			if observation.ObservedAt.After(lastAt) {
				lastAt = observation.ObservedAt
			}
		}
	}
	secondsSinceFirst, secondsSinceLast, calmSeconds := float32(0), float32(0), float32(0)
	if !firstAt.IsZero() {
		secondsSinceFirst = nonNegativeSeconds(input.At.Sub(firstAt))
		secondsSinceLast = nonNegativeSeconds(input.At.Sub(lastAt))
		calmSeconds = secondsSinceLast
	}

	access := stateFrameV5Access(topology, trigger)
	alarm := cognitive.V5AlarmUnknown
	if v4Frame.System.Armed {
		alarm = cognitive.V5AlarmArmed
	} else if v4Frame.SecurityMode != "" {
		alarm = cognitive.V5AlarmDisarmed
	}
	return cognitive.StateFrameV5{
		CapturedAt:    input.At.UTC(),
		Security:      cognitive.StateFrameV5Security{Armed: v4Frame.System.Armed, Degraded: v4Frame.System.Degraded, Known: v4Frame.SecurityMode != ""},
		Presence:      cognitive.StateFrameV5Presence{HumanPresent: humanPresent, TrackCount: len(latestByTrack), TrackConfirmed: trackConfirmed},
		TopologyClass: topology, Priority: priority, PriorityOrigin: cognitive.V5PriorityOriginVision,
		EpisodePhase: phase, Enrichment: enrichment,
		Continuity: cognitive.StateFrameV5Continuity{SecondsSinceFirstObservation: secondsSinceFirst, SecondsSinceLastObservation: secondsSinceLast, SegmentCount: segmentCount, GapCount: gapCount, CalmSeconds: calmSeconds},
		Quality:    cognitive.StateFrameV5Quality{RealDetection: realDetection, ReplaySimulation: len(segments) > 0, ObservationCount: len(eligible), AggregateConfidence: aggregateConfidence},
		CoEvidence: cognitive.StateFrameV5CoEvidence{AccessState: access, Movement: strings.Contains(strings.ToLower(trigger), "motion"), SensorEvidence: strings.TrimSpace(trigger) != "", AlarmState: alarm},
	}
}

func stateFrameV5Phase(name string) string {
	switch name {
	case transitionCandidate:
		return cognitive.V5PhaseCandidate
	case transitionConfirmed:
		return cognitive.V5PhaseConfirmed
	case transitionFinal:
		return cognitive.V5PhaseFinal
	default:
		return cognitive.V5PhaseInitial
	}
}

func stateFrameV5Enrichment(observation *visionObservation, trackCount int) string {
	if trackCount == 0 {
		return cognitive.V5EnrichmentNotRequested
	}
	for _, track := range observation.Tracks {
		switch track.State {
		case "recognized_stable":
			return cognitive.V5EnrichmentRecognized
		case "unknown":
			return cognitive.V5EnrichmentUnknown
		case "uncertain", "enriching":
			return cognitive.V5EnrichmentUncertain
		}
	}
	return cognitive.V5EnrichmentNotRequested
}

func stateFrameV5SegmentCount(input transitionInput, segments []contract.VisionSegmentReadyV1) int {
	if input.Name == transitionFinal {
		return len(segments)
	}
	count := 0
	for _, segment := range segments {
		if !segment.StartedAt.After(input.At) {
			count++
		}
	}
	return count
}

func stateFrameV5GapCount(observations []visionObservation, segments []contract.VisionSegmentReadyV1) int {
	if len(observations) < 2 || len(segments) == 0 {
		return 0
	}
	seen := map[int]bool{}
	for _, observation := range observations {
		for _, segment := range segments {
			if !observation.ObservedAt.Before(segment.StartedAt) && observation.ObservedAt.Before(segment.EndedAt) {
				seen[segment.SegmentIndex] = true
				break
			}
		}
	}
	if len(seen) < 2 {
		return 0
	}
	minIndex, maxIndex := math.MaxInt, 0
	for index := range seen {
		if index < minIndex {
			minIndex = index
		}
		if index > maxIndex {
			maxIndex = index
		}
	}
	return maxIndex - minIndex + 1 - len(seen)
}

func stateFrameV5Access(topology, trigger string) string {
	switch topology {
	case contract.VisionTopologyProtectedInterior, contract.VisionTopologyRestrictedThreshold:
		if strings.Contains(strings.ToLower(trigger), "forced") {
			return cognitive.V5AccessForced
		}
		return cognitive.V5AccessOpen
	case contract.VisionTopologyPrivatePerimeter, contract.VisionTopologyPublicOutdoor:
		return cognitive.V5AccessClosed
	default:
		return cognitive.V5AccessUnknown
	}
}

func nonNegativeSeconds(value time.Duration) float32 {
	if value <= 0 {
		return 0
	}
	return float32(value.Seconds())
}

type stateFrameV5EvidenceRecord struct {
	HumanPresent        bool    `json:"human_present"`
	TrackCount          int     `json:"track_count"`
	TrackConfirmed      bool    `json:"track_confirmed"`
	ObservationCount    int     `json:"observation_count"`
	SegmentCount        int     `json:"segment_count"`
	GapCount            int     `json:"gap_count"`
	RealDetection       bool    `json:"real_detection"`
	ReplaySimulation    bool    `json:"replay_simulation"`
	AggregateConfidence float32 `json:"aggregate_confidence"`
}

func stateFrameV5Evidence(frame cognitive.StateFrameV5) stateFrameV5EvidenceRecord {
	return stateFrameV5EvidenceRecord{
		HumanPresent: frame.Presence.HumanPresent, TrackCount: frame.Presence.TrackCount, TrackConfirmed: frame.Presence.TrackConfirmed,
		ObservationCount: frame.Quality.ObservationCount, SegmentCount: frame.Continuity.SegmentCount, GapCount: frame.Continuity.GapCount,
		RealDetection: frame.Quality.RealDetection, ReplaySimulation: frame.Quality.ReplaySimulation, AggregateConfidence: frame.Quality.AggregateConfidence,
	}
}
