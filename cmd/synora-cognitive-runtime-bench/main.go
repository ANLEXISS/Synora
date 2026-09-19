package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"synora/internal/cognitivecore"
	"synora/internal/discovery"
	"synora/pkg/contract"
)

type runtimeReport struct {
	Schema                    string               `json:"schema"`
	Bundle                    string               `json:"bundle"`
	Iterations                int                  `json:"iterations"`
	ColdBundleLoadMS          float64              `json:"cold_bundle_load_ms"`
	CoreInitMS                float64              `json:"core_init_ms"`
	FirstDecisionMS           float64              `json:"first_decision_ms"`
	SteadyCoreCompleteMS      Quantiles            `json:"steady_core_complete_ms"`
	HeadLatencyMS             map[string]Quantiles `json:"head_latency_ms"`
	RSSMB                     map[string]float64   `json:"rss_mb"`
	StoreCommitMS             Quantiles            `json:"store_commit_ms"`
	SimulatedActionDispatchMS Quantiles            `json:"simulated_action_dispatch_ms"`
	MLPMode                   string               `json:"mlp_mode"`
	Status                    string               `json:"status"`
	PhysicalActionExecuted    bool                 `json:"physical_action_executed"`
	VisionRuntimeSeparated    bool                 `json:"vision_runtime_measured_separately"`
}

type Quantiles struct {
	P50 float64 `json:"p50_ms"`
	P95 float64 `json:"p95_ms"`
	P99 float64 `json:"p99_ms"`
}

func main() {
	bundleDir := flag.String("bundle", "", "CPU MLP bundle directory")
	iterations := flag.Int("iterations", 128, "steady-state iterations")
	out := flag.String("out", "", "optional JSON output path")
	flag.Parse()
	if strings.TrimSpace(*bundleDir) == "" || *iterations < 8 {
		fatal("bundle and at least eight iterations are required")
	}

	preRSS := rssMB()
	started := time.Now()
	mlp, err := cognitivecore.LoadCPUBundle(*bundleDir)
	if err != nil {
		fatal(err.Error())
	}
	coldLoad := elapsedMS(started)
	postRSS := rssMB()

	initStarted := time.Now()
	store := cognitivecore.NewUniversalStore()
	core := &cognitivecore.Core{Store: store, MLP: mlp, Gate: cognitivecore.SafetyGate{DryRun: true}, Now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	coreInit := elapsedMS(initStarted)

	firstStarted := time.Now()
	first, err := core.Process(context.Background(), benchEvent(0))
	if err != nil {
		fatal(err.Error())
	}
	firstDecision := elapsedMS(firstStarted)

	steady := make([]float64, 0, *iterations)
	headValues := make(map[string][]float64, len(cognitivecore.HeadOrder))
	for _, head := range cognitivecore.HeadOrder {
		headValues[head] = make([]float64, 0, *iterations)
	}
	actionValues := make([]float64, 0, *iterations)
	for i := 1; i <= *iterations; i++ {
		started = time.Now()
		result, processErr := core.Process(context.Background(), benchEvent(i))
		if processErr != nil {
			fatal(processErr.Error())
		}
		steady = append(steady, elapsedMS(started))
		for head, value := range result.Commit.Decision.HeadLatencyMS {
			headValues[head] = append(headValues[head], value)
		}
		if result.Result.Action != nil {
			actionStarted := time.Now()
			if _, actionErr := (&discovery.Boundary{DryRun: true, Capabilities: map[string]bool{"notify": true, "record": true}}).ExecuteAction(discovery.ActionRequest{SchemaVersion: discovery.BoundarySchemaVersion, RequestID: strconv.Itoa(i), Action: result.Result.Action.Action.Action, Capability: result.Result.Action.Action.Capability, DryRun: true}); actionErr != nil {
				fatal(actionErr.Error())
			}
			actionValues = append(actionValues, elapsedMS(actionStarted))
		}
	}
	stableRSS := rssMB()

	commitValues := make([]float64, 0, *iterations)
	commitStore := cognitivecore.NewUniversalStore()
	for i := 0; i < *iterations; i++ {
		started = time.Now()
		_, err := commitStore.Commit(cognitivecore.Commit{Event: benchEvent(10000 + i), Snapshot: cognitivecore.CognitiveSnapshot{SchemaVersion: cognitivecore.SnapshotSchemaVersion, CapturedAt: time.Unix(int64(i+1), 0).UTC(), Topology: contract.VisionTopologyUnknown, Episode: cognitivecore.EpisodeFacts{Phase: "initial"}}, Decision: cognitivecore.Decision{SchemaVersion: cognitivecore.DecisionSchemaVersion, Status: "unavailable", Mode: "active_dry_run"}, CommittedAt: time.Unix(int64(i+1), 0).UTC()})
		if err != nil {
			fatal(err.Error())
		}
		commitValues = append(commitValues, elapsedMS(started))
	}

	report := runtimeReport{Schema: "synora.cognitive-runtime-benchmark/v1", Bundle: *bundleDir, Iterations: *iterations, ColdBundleLoadMS: coldLoad, CoreInitMS: coreInit, FirstDecisionMS: firstDecision, SteadyCoreCompleteMS: quantiles(steady), HeadLatencyMS: make(map[string]Quantiles, len(headValues)), RSSMB: map[string]float64{"before_bundle": preRSS, "after_bundle": postRSS, "steady": stableRSS}, StoreCommitMS: quantiles(commitValues), SimulatedActionDispatchMS: quantiles(actionValues), MLPMode: first.Commit.Decision.Mode, Status: first.Commit.Decision.Status, PhysicalActionExecuted: false, VisionRuntimeSeparated: true}
	for head, values := range headValues {
		report.HeadLatencyMS[head] = quantiles(values)
	}
	body, _ := json.MarshalIndent(report, "", "  ")
	body = append(body, '\n')
	if *out != "" {
		if err := os.WriteFile(*out, body, 0o640); err != nil {
			fatal(err.Error())
		}
	}
	fmt.Print(string(body))
}

func benchEvent(index int) contract.Event {
	return contract.Event{ID: fmt.Sprintf("runtime-bench-%d", index), Type: contract.EventVisionSegmentReadyV1, Source: "discovery", Timestamp: time.Unix(int64(1700000000+index), 0).UTC(), Payload: map[string]any{"topology": contract.VisionTopologyPrivatePerimeter, "human_present": true, "track_count": 1, "track_confirmed": index > 2, "priority": contract.VisionPriorityP1, "episode_phase": map[bool]string{true: "confirmed", false: "candidate"}[index > 2], "segment_count": index + 1, "observation_count": index + 1, "confidence": 0.85, "real_detection": true}}
}

func quantiles(values []float64) Quantiles {
	if len(values) == 0 {
		return Quantiles{}
	}
	copyValues := append([]float64(nil), values...)
	for i := 1; i < len(copyValues); i++ {
		value := copyValues[i]
		j := i - 1
		for j >= 0 && copyValues[j] > value {
			copyValues[j+1] = copyValues[j]
			j--
		}
		copyValues[j+1] = value
	}
	pick := func(percent float64) float64 {
		index := int(float64(len(copyValues)-1) * percent)
		return copyValues[index]
	}
	return Quantiles{P50: pick(.50), P95: pick(.95), P99: pick(.99)}
}

func rssMB() float64 {
	body, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				value, _ := strconv.ParseFloat(fields[1], 64)
				return value / 1024
			}
		}
	}
	return 0
}

func elapsedMS(start time.Time) float64 { return float64(time.Since(start).Microseconds()) / 1000 }

func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
