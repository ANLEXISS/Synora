package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/pkg/contract"
)

type replayObservation struct {
	Type    string         `json:"type"`
	TrackID any            `json:"track_id,omitempty"`
	Payload map[string]any `json:"payload"`
}

type replayReport struct {
	SchemaVersion          string         `json:"schema_version"`
	Source                 string         `json:"source"`
	Frames                 int            `json:"frames"`
	Events                 int            `json:"events"`
	Commits                int            `json:"commits"`
	Duplicates             int            `json:"duplicates"`
	ActiveDryRun           bool           `json:"active_dry_run"`
	PhysicalActionExecuted bool           `json:"physical_action_executed"`
	StoreRevision          uint64         `json:"store_revision"`
	Observations           []string       `json:"observations"`
	VisionModelReal        bool           `json:"vision_model_real"`
	ModelLoaded            bool           `json:"model_loaded"`
	ModelPath              string         `json:"model_path,omitempty"`
	Detections             int            `json:"detections"`
	Segments               int            `json:"segments"`
	Tracks                 int            `json:"tracks"`
	VisionObservations     int            `json:"vision_observations"`
	VisionLatencies        map[string]any `json:"vision_latencies,omitempty"`
}

type visionReport struct {
	VisionModelReal bool           `json:"vision_model_real"`
	ModelLoaded     bool           `json:"model_loaded"`
	ModelPath       string         `json:"model_path"`
	Detections      int            `json:"detections"`
	Segments        int            `json:"segments"`
	Tracks          int            `json:"tracks"`
	Observations    int            `json:"observations"`
	Latencies       map[string]any `json:"latencies"`
}

func main() {
	observationsPath := flag.String("observations", "", "Vision observation JSONL")
	outPath := flag.String("out", "", "replay report JSON")
	visionReportPath := flag.String("vision-report", "", "real Vision report JSON")
	flag.Parse()
	if strings.TrimSpace(*observationsPath) == "" || strings.TrimSpace(*outPath) == "" {
		fatal("--observations and --out are required")
	}
	file, err := os.Open(*observationsPath)
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: cognitivecore.UnavailableMLP{Reason: "no promoted full-snapshot V1 bundle"}, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Now().UTC() }}
	report := replayReport{SchemaVersion: "synora.v1-replay/v1", Source: *observationsPath, ActiveDryRun: true, PhysicalActionExecuted: false, Observations: []string{}, VisionLatencies: map[string]any{}}
	boundary := &discovery.Boundary{DryRun: true}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	sequence := 0
	for scanner.Scan() {
		var observation replayObservation
		if err := json.Unmarshal(scanner.Bytes(), &observation); err != nil {
			fatal(fmt.Sprintf("decode observation: %v", err))
		}
		if observation.Type == "" {
			continue
		}
		sequence++
		eventID := fmt.Sprintf("replay-%06d", sequence)
		event, err := boundary.NormalizeEvent(contract.Event{ID: eventID, Type: observation.Type, Source: "vision-worker", Timestamp: time.Now().UTC(), Payload: observation.Payload})
		if err != nil {
			fatal(fmt.Sprintf("Discovery rejected Vision observation: %v", err))
		}
		result, err := core.Process(context.Background(), event)
		if err != nil {
			fatal(err.Error())
		}
		report.Events++
		report.Frames++
		if result.Result.Duplicate {
			report.Duplicates++
		} else {
			report.Commits++
		}
		report.Observations = append(report.Observations, observation.Type)
	}
	if err := scanner.Err(); err != nil {
		fatal(err.Error())
	}
	report.StoreRevision = store.Revision()
	if *visionReportPath != "" {
		body, err := os.ReadFile(*visionReportPath)
		if err != nil {
			fatal(fmt.Sprintf("read Vision report: %v", err))
		}
		var vision visionReport
		if err := json.Unmarshal(body, &vision); err != nil {
			fatal(fmt.Sprintf("decode Vision report: %v", err))
		}
		report.VisionModelReal, report.ModelLoaded, report.ModelPath = vision.VisionModelReal, vision.ModelLoaded, vision.ModelPath
		report.Detections, report.Segments, report.Tracks, report.VisionObservations = vision.Detections, vision.Segments, vision.Tracks, vision.Observations
		report.VisionLatencies = vision.Latencies
	}
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(*outPath, append(body, '\n'), 0o640); err != nil {
		fatal(err.Error())
	}
}

func fatal(message string) { panic(errors.New(message)) }
