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
	"synora/pkg/contract"
)

type replayObservation struct {
	Type    string         `json:"type"`
	TrackID any            `json:"track_id,omitempty"`
	Payload map[string]any `json:"payload"`
}

type replayReport struct {
	SchemaVersion          string   `json:"schema_version"`
	Source                 string   `json:"source"`
	Frames                 int      `json:"frames"`
	Events                 int      `json:"events"`
	Commits                int      `json:"commits"`
	Duplicates             int      `json:"duplicates"`
	ActiveDryRun           bool     `json:"active_dry_run"`
	PhysicalActionExecuted bool     `json:"physical_action_executed"`
	StoreRevision          uint64   `json:"store_revision"`
	Observations           []string `json:"observations"`
}

func main() {
	observationsPath := flag.String("observations", "", "Vision observation JSONL")
	outPath := flag.String("out", "", "replay report JSON")
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
	report := replayReport{SchemaVersion: "synora.v1-replay/v1", Source: *observationsPath, ActiveDryRun: true, PhysicalActionExecuted: false, Observations: []string{}}
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
		result, err := core.Process(context.Background(), contract.Event{ID: eventID, Type: observation.Type, Source: "discovery", Timestamp: time.Now().UTC(), Payload: observation.Payload})
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
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	if err := os.WriteFile(*outPath, append(body, '\n'), 0o640); err != nil {
		fatal(err.Error())
	}
}

func fatal(message string) { panic(errors.New(message)) }
