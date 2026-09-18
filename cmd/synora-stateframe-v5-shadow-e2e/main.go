package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"synora/internal/cognitive"
	"synora/pkg/contract"
)

const fixtureSchema = "synora.stateframe-v5-fixture/v1"

type fixture struct {
	Schema      string                 `json:"schema"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	State       cognitive.StateFrameV5 `json:"state"`
	Expected    fixtureExpected        `json:"expected"`
}

type fixtureExpected struct {
	Topology       string `json:"topology"`
	Priority       string `json:"priority"`
	Phase          string `json:"phase"`
	Enrichment     string `json:"enrichment"`
	HumanPresent   bool   `json:"human_present"`
	TrackCount     int    `json:"track_count"`
	TrackConfirmed bool   `json:"track_confirmed"`
	SegmentCount   int    `json:"segment_count"`
	GapCount       int    `json:"gap_count"`
}

type fixtureResult struct {
	Name             string  `json:"name"`
	V4Dimension      int     `json:"v4_dimension"`
	V5Dimension      int     `json:"v5_dimension"`
	V4LatencyMS      float64 `json:"v4_latency_ms"`
	V5LatencyMS      float64 `json:"v5_latency_ms"`
	V4Fingerprint    string  `json:"v4_fingerprint"`
	V5Fingerprint    string  `json:"v5_fingerprint"`
	V5Stable         bool    `json:"v5_stable"`
	V5ForwardedToMLP bool    `json:"v5_forwarded_to_mlp"`
	PhysicalAction   bool    `json:"physical_action_executed"`
}

type report struct {
	Schema                 string          `json:"schema"`
	V4Schema               string          `json:"v4_schema"`
	V4Dimension            int             `json:"v4_dimension"`
	V5Schema               string          `json:"v5_schema"`
	V5Dimension            int             `json:"v5_dimension"`
	MLPInputSchema         string          `json:"mlp_input_schema"`
	V5ForwardedToMLP       bool            `json:"v5_forwarded_to_mlp"`
	AdvisoryShadow         bool            `json:"advisory_shadow"`
	PhysicalActionExecuted bool            `json:"physical_action_executed"`
	Results                []fixtureResult `json:"results"`
}

func main() {
	fixtures := flag.String("fixtures", "testdata/stateframe-v5", "StateFrame V5 fixture directory")
	out := flag.String("out", "/tmp/synora-stateframe-v5-shadow-v1", "report output directory")
	flag.Parse()
	if err := run(*fixtures, *out); err != nil {
		fmt.Fprintln(os.Stderr, "synora-stateframe-v5-shadow-e2e:", err)
		os.Exit(1)
	}
}

func run(fixturesDir, outDir string) error {
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			paths = append(paths, filepath.Join(fixturesDir, entry.Name()))
		}
	}
	sort.Strings(paths)
	if len(paths) < 10 {
		return fmt.Errorf("want at least 10 stateframe-v5 fixtures, got %d", len(paths))
	}
	results := make([]fixtureResult, 0, len(paths))
	captures := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		var item fixture
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &item); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
		if item.Schema != fixtureSchema || item.Name == "" {
			return fmt.Errorf("invalid fixture %s", path)
		}
		result, capture, err := checkFixture(item)
		if err != nil {
			return fmt.Errorf("fixture %s: %w", item.Name, err)
		}
		results = append(results, result)
		captures = append(captures, capture)
	}
	value := report{
		Schema: "synora.stateframe-v5-shadow-e2e/v1", V4Schema: cognitive.StateEncoderSchemaVersion, V4Dimension: cognitive.EncoderV4Size,
		V5Schema: cognitive.StateEncoderV5SchemaVersion, V5Dimension: cognitive.EncoderV5Size,
		MLPInputSchema: cognitive.StateEncoderSchemaVersion, V5ForwardedToMLP: false, AdvisoryShadow: true, PhysicalActionExecuted: false,
		Results: results,
	}
	if err := writeJSON(filepath.Join(outDir, "report.json"), value); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "stateframe-v5-fixture-capture.jsonl"), captures); err != nil {
		return err
	}
	for _, result := range results {
		if result.V5Dimension != cognitive.EncoderV5Size || result.V4Dimension != cognitive.EncoderV4Size || !result.V5Stable || result.V5ForwardedToMLP || result.PhysicalAction {
			return fmt.Errorf("shadow invariant failed for fixture %s", result.Name)
		}
	}
	fmt.Printf("stateframe-v5-shadow: fixtures=%d v4=%d v5=%d physical_action_executed=false\n", len(results), cognitive.EncoderV4Size, cognitive.EncoderV5Size)
	return nil
}

func checkFixture(item fixture) (fixtureResult, map[string]any, error) {
	frame := item.State.Normalized()
	if err := frame.Validate(); err != nil {
		return fixtureResult{}, nil, err
	}
	if err := checkExpected(frame, item.Expected); err != nil {
		return fixtureResult{}, nil, err
	}
	v5Encoder := cognitive.V5StateEncoder{}
	v5Start := time.Now()
	v5, err := v5Encoder.Encode(context.Background(), frame)
	v5Latency := float64(time.Since(v5Start).Microseconds()) / 1000
	if err != nil {
		return fixtureResult{}, nil, err
	}
	v5Again, err := v5Encoder.Encode(context.Background(), frame)
	if err != nil {
		return fixtureResult{}, nil, err
	}
	v5Fingerprint, err := frame.Fingerprint()
	if err != nil {
		return fixtureResult{}, nil, err
	}
	if v5.Values != v5Again.Values {
		return fixtureResult{}, nil, errors.New("V5 vector is not stable")
	}
	if frame.Priority == contract.VisionPriorityP0 {
		visionFrame := frame
		visionFrame.PriorityOrigin = cognitive.V5PriorityOriginVision
		if _, visionErr := v5Encoder.Encode(context.Background(), visionFrame); visionErr == nil {
			return fixtureResult{}, nil, errors.New("Vision was allowed to create P0")
		}
	}
	v4Frame := v4FrameFromV5(frame)
	v4Start := time.Now()
	v4, err := (cognitive.V4StateEncoder{}).Encode(context.Background(), v4Frame)
	v4Latency := float64(time.Since(v4Start).Microseconds()) / 1000
	if err != nil {
		return fixtureResult{}, nil, err
	}
	v4Fingerprint := v4.FrameChecksum
	result := fixtureResult{Name: item.Name, V4Dimension: cognitive.EncoderV4Size, V5Dimension: cognitive.EncoderV5Size, V4LatencyMS: v4Latency, V5LatencyMS: v5Latency, V4Fingerprint: v4Fingerprint, V5Fingerprint: v5Fingerprint, V5Stable: true, V5ForwardedToMLP: false, PhysicalAction: false}
	capture := map[string]any{
		"schema": cognitive.StateFrameV5CaptureSchema, "fixture": item.Name, "state_encoder_schema": cognitive.StateEncoderV5SchemaVersion,
		"state_encoder_version": cognitive.EncoderV5Version, "feature_dimension": cognitive.EncoderV5Size, "feature_order": cognitive.EncoderV5FeatureNames,
		"state": frame, "vector": v5.Values, "v5_fingerprint": v5Fingerprint,
		"v4":         map[string]any{"schema": cognitive.StateEncoderSchemaVersion, "dimension": cognitive.EncoderV4Size, "frame_fingerprint": v4Fingerprint},
		"provenance": map[string]any{"source": "stateframe_v5_fixture", "cognitive_mode": "advisory_shadow", "physical_action_executed": false},
	}
	return result, capture, nil
}

func checkExpected(frame cognitive.StateFrameV5, expected fixtureExpected) error {
	if frame.TopologyClass != expected.Topology || frame.Priority != expected.Priority || frame.EpisodePhase != expected.Phase || frame.Enrichment != expected.Enrichment || frame.Presence.HumanPresent != expected.HumanPresent || frame.Presence.TrackCount != expected.TrackCount || frame.Presence.TrackConfirmed != expected.TrackConfirmed || frame.Continuity.SegmentCount != expected.SegmentCount || frame.Continuity.GapCount != expected.GapCount {
		return fmt.Errorf("expected facts do not match normalized state: %#v", frame)
	}
	return nil
}

func v4FrameFromV5(frame cognitive.StateFrameV5) cognitive.StateFrame {
	event := (*cognitive.StateEvent)(nil)
	if frame.Presence.HumanPresent {
		event = &cognitive.StateEvent{Type: "motion.person.detected", Confidence: float64(frame.Quality.AggregateConfidence), Timestamp: frame.CapturedAt}
	}
	return cognitive.StateFrame{
		SchemaVersion: cognitive.StateFrameSchemaVersion, CapturedAt: frame.CapturedAt,
		CurrentEvent: event, System: cognitive.StateSystem{Armed: frame.Security.Armed, Degraded: frame.Security.Degraded},
		SecurityMode: map[bool]string{true: "armed_away", false: "disarmed"}[frame.Security.Armed],
		Topology:     []cognitive.StateNode{{Type: frame.TopologyClass}}, RecentEvents: []cognitive.StateEvent{},
		Temporal: cognitive.EncoderV4TemporalContext{EvaluationTrigger: "event", QuietSeconds: frame.Continuity.CalmSeconds, SecondsSinceLastEvent: frame.Continuity.SecondsSinceLastObservation, EventCount: frame.Quality.ObservationCount, SequenceDurationSeconds: frame.Continuity.SecondsSinceFirstObservation},
	}
}

func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(body, '\n'), 0o640)
}

func writeJSONL(path string, values []map[string]any) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, value := range values {
		body, err := json.Marshal(value)
		if err != nil {
			return err
		}
		lower := strings.ToLower(string(body))
		for _, token := range []string{"bbox", "crop", "embedding", "biometric", "identity", "face_image", "raw_frame", "track_id", "camera_id", "device_id", "episode_id", ".mp4", "jpeg", "png"} {
			if strings.Contains(lower, token) {
				return fmt.Errorf("forbidden token %q in V5 capture", token)
			}
		}
		if _, err := file.Write(append(body, '\n')); err != nil {
			return err
		}
	}
	return nil
}
