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
	SchemaVersion          string             `json:"schema_version"`
	Source                 string             `json:"source"`
	Frames                 int                `json:"frames"`
	Events                 int                `json:"events"`
	Commits                int                `json:"commits"`
	Duplicates             int                `json:"duplicates"`
	ActiveDryRun           bool               `json:"active_dry_run"`
	PhysicalActionExecuted bool               `json:"physical_action_executed"`
	StoreRevision          uint64             `json:"store_revision"`
	Observations           []string           `json:"observations"`
	VisionModelReal        bool               `json:"vision_model_real"`
	ModelLoaded            bool               `json:"model_loaded"`
	ModelPath              string             `json:"model_path,omitempty"`
	Detections             int                `json:"detections"`
	Segments               int                `json:"segments"`
	Tracks                 int                `json:"tracks"`
	VisionObservations     int                `json:"vision_observations"`
	VisionLatencies        map[string]any     `json:"vision_latencies,omitempty"`
	ModelStatus            string             `json:"model_status"`
	ModelBundle            string             `json:"model_bundle,omitempty"`
	ModelError             string             `json:"model_error,omitempty"`
	DecisionStatuses       map[string]int     `json:"decision_statuses"`
	Actions                map[string]int     `json:"actions"`
	MLPHeadLatencyMS       map[string]float64 `json:"mlp_head_latency_ms"`
	StorePersistent        bool               `json:"store_persistent"`
	StoreRestarted         bool               `json:"store_restarted"`
	StoreReplayIdentical   bool               `json:"store_replay_identical"`
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
	bundlePath := flag.String("bundle", os.Getenv("SYNORA_COGNITIVE_BUNDLE"), "V1 MLP bundle directory")
	storePath := flag.String("store-dir", os.Getenv("SYNORA_STORE_DIR"), "durable Store directory")
	flag.Parse()
	if strings.TrimSpace(*observationsPath) == "" || strings.TrimSpace(*outPath) == "" {
		fatal("--observations and --out are required")
	}
	file, err := os.Open(*observationsPath)
	if err != nil {
		fatal(err.Error())
	}
	defer file.Close()
	var store *cognitivecore.UniversalStore
	if strings.TrimSpace(*storePath) != "" {
		store, err = cognitivecore.OpenUniversalStore(*storePath)
	} else {
		store = cognitivecore.NewUniversalStore()
	}
	var mlp cognitivecore.MLPBackend = cognitivecore.UnavailableMLP{Reason: "no promoted full-snapshot V1 bundle"}
	report := replayReport{SchemaVersion: "synora.v1-replay/v1", Source: *observationsPath, ActiveDryRun: true, PhysicalActionExecuted: false, Observations: []string{}, VisionLatencies: map[string]any{}, ModelStatus: "unavailable", DecisionStatuses: map[string]int{}, Actions: map[string]int{}, MLPHeadLatencyMS: map[string]float64{}, StorePersistent: strings.TrimSpace(*storePath) != ""}
	if err != nil {
		fatal(fmt.Sprintf("open durable Store: %v", err))
	}
	if strings.TrimSpace(*bundlePath) != "" {
		loaded, loadErr := cognitivecore.LoadCPUBundle(*bundlePath)
		report.ModelBundle = *bundlePath
		if loadErr != nil {
			report.ModelStatus, report.ModelError = "incompatible", loadErr.Error()
		} else {
			mlp, report.ModelStatus = loaded, "loaded"
		}
	}
	core := &cognitivecore.Core{Store: store, MLP: mlp, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Now().UTC() }}
	boundary := &discovery.Boundary{DryRun: true}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	sequence := 0
	processedEvents := make([]contract.Event, 0)
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
		report.DecisionStatuses[result.Commit.Decision.Status]++
		report.Actions[result.Commit.Decision.Action.Proposed.Action]++
		for head, latency := range result.Commit.Decision.HeadLatencyMS {
			report.MLPHeadLatencyMS[head] += latency
		}
		processedEvents = append(processedEvents, event)
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
	if report.StorePersistent {
		restarted, restartErr := cognitivecore.OpenUniversalStore(*storePath)
		if restartErr != nil {
			fatal(fmt.Sprintf("restart durable Store: %v", restartErr))
		}
		report.StoreRestarted = true
		replayCore := &cognitivecore.Core{Store: restarted, MLP: mlp, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Now().UTC() }}
		replayIdentical := restarted.Revision() == store.Revision() && len(restarted.Journal()) == report.Commits
		for _, event := range processedEvents {
			result, replayErr := replayCore.Process(context.Background(), event)
			if replayErr != nil || !result.Result.Duplicate {
				replayIdentical = false
				break
			}
		}
		report.StoreReplayIdentical = replayIdentical
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
