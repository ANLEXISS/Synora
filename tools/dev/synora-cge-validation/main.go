// synora-cge-validation is a development-only qualification harness. It is
// intentionally not wired into any runtime, service, or startup path.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"synora/internal/cge/campaign"
	"synora/internal/cge/validation"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "list":
		list(os.Args[2:])
	case "run":
		runOne(os.Args[2:])
	case "run-all":
		runAll(os.Args[2:])
	case "qualify":
		qualify(os.Args[2:])
	case "benchmark":
		benchmark(os.Args[2:])
	case "campaign":
		campaignCommand(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func campaignCommand(args []string) {
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		campaignList(args[1:])
	case "run":
		campaignRun(args[1:])
	case "run-all":
		campaignRunAll(args[1:])
	default:
		usage()
		os.Exit(2)
	}
}

func campaignList(args []string) {
	if hasJSON(args) {
		printJSON(campaign.DefaultProfiles())
		return
	}
	for _, profile := range campaign.DefaultProfiles() {
		fmt.Printf("%s\t%s\n", profile.ID, profile.Description)
	}
}

func campaignRun(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "campaign run requires a profile id")
		os.Exit(2)
	}
	options := parseOptions(args[1:])
	profile, ok := campaign.ProfileByID(args[0])
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown campaign profile", args[0])
		os.Exit(2)
	}
	root, cleanup := dataRoot(options)
	defer cleanup()
	days := options.days
	if !options.full && days == 0 {
		days = 7
	}
	report, err := campaign.Run(context.Background(), profile, campaign.RunOptions{RootDir: filepath.Join(root, profile.ID), Full: options.full, DaysOverride: days, EventsOutput: options.eventsOutput})
	if options.json {
		printJSON(report)
	} else {
		printCampaignText(report)
	}
	if options.jsonPath != "" {
		writeJSONFile(options.jsonPath, report)
	}
	if err != nil || !report.Success {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func campaignRunAll(args []string) {
	options := parseOptions(args)
	root, cleanup := dataRoot(options)
	defer cleanup()
	reports := make([]campaign.Report, 0)
	days := options.days
	if !options.full && days == 0 {
		days = 7
	}
	for _, profile := range campaign.DefaultProfiles() {
		report, err := campaign.Run(context.Background(), profile, campaign.RunOptions{RootDir: filepath.Join(root, profile.ID), Full: options.full, DaysOverride: days, EventsOutput: eventsPath(options.eventsOutput, profile.ID)})
		if err != nil {
			if options.json {
				printJSON(report)
			} else {
				fmt.Printf("campaign %s error %v\n", profile.ID, err)
			}
			os.Exit(1)
		}
		reports = append(reports, report)
	}
	if options.json {
		printJSON(reports)
	} else {
		for _, report := range reports {
			printCampaignText(report)
		}
	}
	if options.jsonPath != "" {
		writeJSONFile(options.jsonPath, reports)
	}
	for _, report := range reports {
		if !report.Success {
			os.Exit(1)
		}
	}
}

func eventsPath(base, profileID string) string {
	if base == "" {
		return ""
	}
	return filepath.Join(base, profileID+".ndjson")
}

func printCampaignText(report campaign.Report) {
	fmt.Printf("campaign %s profile=%s seed=%d simulated=%s..%s events=%d succeeded=%d failed=%d warmup_events=%d insufficient=%d restarts=%d checkpoints=%d routines=%d occurrences=%d journal_records=%d journal_bytes=%d latency_p50=%s latency_p95=%s invariant_failures=%d findings=%d success=%t\n", report.CampaignID, report.ProfileID, report.Seed, report.SimulatedStart.Format(time.RFC3339), report.SimulatedEnd.Format(time.RFC3339), report.EventCount, report.EventsSucceeded, report.EventsFailed, report.Warmup.EventsBeforeFirstEvaluation, report.Warmup.InsufficientHistoryCount, report.RestartCount, report.CheckpointCount, lastGrowth(report.Growth).RoutineCount, lastGrowth(report.Growth).RoutineOccurrenceCount, lastGrowth(report.Growth).JournalRecords, lastGrowth(report.Growth).JournalBytes, report.Latency.Total.Median, report.Latency.Total.P95, len(report.InvariantFailures), len(report.CalibrationFindings), report.Success)
}

func lastGrowth(values []campaign.GrowthMetrics) campaign.GrowthMetrics {
	if len(values) == 0 {
		return campaign.GrowthMetrics{}
	}
	return values[len(values)-1]
}

func list(args []string) {
	jsonOutput := hasJSON(args)
	items := validation.Catalog()
	if jsonOutput {
		printJSON(items)
		return
	}
	for _, item := range items {
		fmt.Printf("%s\t%s\n", item.ID, item.Description)
	}
}

func runOne(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "run requires a scenario id")
		os.Exit(2)
	}
	options := parseOptions(args[1:])
	scenario, err := validation.FindScenario(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	root, cleanup := dataRoot(options)
	defer cleanup()
	report, runErr := (&validation.Runner{RootDir: root}).Run(context.Background(), scenario)
	writeReport(report, options.jsonPath, options.json)
	if runErr != nil || !report.Success {
		os.Exit(1)
	}
}

func runAll(args []string) {
	options := parseOptions(args)
	root, cleanup := dataRoot(options)
	defer cleanup()
	reports, runErr := (&validation.Runner{RootDir: root}).RunCatalog(context.Background())
	if options.json {
		printJSON(reports)
	} else {
		for _, report := range reports {
			printText(report)
		}
	}
	if options.jsonPath != "" {
		writeJSONFile(options.jsonPath, reports)
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, runErr)
		os.Exit(1)
	}
	for _, report := range reports {
		if !report.Success {
			os.Exit(1)
		}
	}
}

func qualify(args []string) {
	options := parseOptions(args)
	root, cleanup := dataRoot(options)
	defer cleanup()
	report, err := validation.RunQualificationMatrixWithOptions(context.Background(), root, validation.QualificationOptions{Full: options.full})
	if options.json {
		printJSON(report)
	} else {
		printQualificationText(report)
	}
	if options.jsonPath != "" {
		writeJSONFile(options.jsonPath, report)
	}
	if err != nil || !report.ReadyForShadowOrchestration || !report.ReadyForCognitiveShadowRuntime || !report.ReadyForContextualRoutineLearning || !report.ReadyForRoutineShadowLearning || !report.ReadyForDeviationShadowIntegration || !report.ReadyForDeviationShadowRuntime || !report.ReadyForRealHouseholdShadowTrial || !report.ReadyForPhysicalShadowDeployment || !report.ReadyForManualInstallation {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func benchmark(args []string) {
	options := parseOptions(args)
	root, cleanup := dataRoot(options)
	defer cleanup()
	report, err := validation.RunPerformanceBenchmark(context.Background(), root, options.full)
	if options.json {
		printJSON(report)
	} else {
		printPerformanceText(report)
	}
	if options.jsonPath != "" {
		writeJSONFile(options.jsonPath, report)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type options struct {
	json         bool
	jsonPath     string
	keep         bool
	full         bool
	days         int
	eventsOutput string
}

func parseOptions(args []string) options {
	set := flag.NewFlagSet("synora-cge-validation", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	jsonOutput := set.Bool("json", false, "emit stable JSON")
	output := set.String("output", "", "write JSON report to a file")
	keep := set.Bool("keep-data", false, "keep the temporary data directory")
	full := set.Bool("full", false, "run exhaustive 500-item qualification")
	days := set.Int("days", 0, "override simulated campaign days")
	eventsOutput := set.String("events-output", "", "write redacted campaign EventResult NDJSON")
	_ = set.Parse(args)
	return options{json: *jsonOutput, jsonPath: *output, keep: *keep, full: *full, days: *days, eventsOutput: *eventsOutput}
}
func hasJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}
func dataRoot(options options) (string, func()) {
	root, err := os.MkdirTemp("", "synora-cge-validation-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if options.keep {
		fmt.Fprintln(os.Stderr, "validation data:", root)
		return root, func() {}
	}
	return root, func() { _ = os.RemoveAll(root) }
}
func writeReport(report validation.ScenarioReport, path string, jsonOutput bool) {
	if jsonOutput {
		printJSON(report)
	} else {
		printText(report)
	}
	if path != "" {
		writeJSONFile(path, report)
	}
}
func writeJSONFile(path string, value any) {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}
func printJSON(value any) {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	fmt.Println(string(data))
}
func printText(report validation.ScenarioReport) {
	passed := 0
	for _, step := range report.Steps {
		if step.Success {
			passed++
		}
	}
	fmt.Printf("scenario %s\nstatus %s\nsteps %d/%d\nchains %d\nhypotheses %d\nreplays performed %d\njournal sequence %d\njournal head %s\ninvariant failures %d\n", report.ScenarioID, status(report.Success), passed, len(report.Steps), report.FinalState.ChainCount, report.FinalState.HypothesisCount, report.Metrics.ReplaysPerformed, report.FinalState.JournalSequence, report.FinalState.JournalHeadHash, len(report.Failures))
	if len(report.Failures) > 0 {
		for _, failure := range report.Failures {
			fmt.Printf("failure %s %s\n", failure.Code, failure.Path)
		}
	}
}
func status(ok bool) string {
	if ok {
		return "passed"
	}
	return "failed"
}
func printQualificationText(report validation.QualificationReport) {
	for _, capability := range report.Capabilities {
		fmt.Printf("%s cognitive=%s transactional=%s wal=%s concurrency=%s checkpoints=%s collisions=%s idempotence=%s status=%s reason=%s\n", capability.Capability, capability.CognitiveReachability, capability.TransactionalQualification, capability.WALFailureQualification, capability.ConcurrencyQualification, capability.CheckpointQualification, capability.CollisionQualification, capability.IdempotenceQualification, qualificationStatus(capability), capability.ReasonCode)
	}
	fmt.Printf("qualification tests %d/%d passed\nready_for_shadow_orchestration %t\nready_for_cognitive_shadow_runtime %t\nready_for_contextual_routine_learning %t\nready_for_routine_shadow_learning %t\nready_for_deviation_shadow_integration %t\nready_for_deviation_shadow_runtime %t\nready_for_real_household_shadow_trial %t\nready_for_physical_shadow_deployment %t\nready_for_manual_installation %t\n", report.PassedTests, report.TotalTests, report.ReadyForShadowOrchestration, report.ReadyForCognitiveShadowRuntime, report.ReadyForContextualRoutineLearning, report.ReadyForRoutineShadowLearning, report.ReadyForDeviationShadowIntegration, report.ReadyForDeviationShadowRuntime, report.ReadyForRealHouseholdShadowTrial, report.ReadyForPhysicalShadowDeployment, report.ReadyForManualInstallation)
	for _, reason := range report.BlockingReasons {
		fmt.Println("blocking", reason)
	}
}

func printPerformanceText(report validation.PerformanceReport) {
	fmt.Printf("performance standard=%t full=%t race=%t ready_for_shadow_performance=%t\n", report.StandardSuiteCompleted, report.FullQualificationCompleted, report.RaceSuiteCompleted, report.ReadyForShadowPerformance)
	for _, benchmark := range report.Benchmarks {
		fmt.Printf("benchmark %s size=%d duration=%s alloc_bytes=%d allocs=%d chains=%d hypotheses=%d routines=%d records=%d journal_bytes=%d\n", benchmark.Name, benchmark.Size, benchmark.Duration, benchmark.AllocBytes, benchmark.Allocations, benchmark.Chains, benchmark.Hypotheses, benchmark.Routines, benchmark.Records, benchmark.JournalBytes)
	}
	for _, hotspot := range report.Hotspots {
		fmt.Printf("hotspot %s category=%s runtime=%t share=%.3f evidence=%s\n", hotspot.Name, hotspot.Category, hotspot.Runtime, hotspot.Share, hotspot.Evidence)
	}
	for _, reason := range report.BlockingReasons {
		fmt.Println("blocking", reason)
	}
}

func qualificationStatus(capability validation.CapabilityQualification) string {
	if capability.TestsFailed == 0 && capability.CognitiveReachability != validation.QualificationFailed && capability.TransactionalQualification != validation.QualificationFailed && capability.WALFailureQualification != validation.QualificationFailed && capability.ConcurrencyQualification != validation.QualificationFailed && capability.CheckpointQualification != validation.QualificationFailed && capability.CollisionQualification != validation.QualificationFailed && capability.IdempotenceQualification != validation.QualificationFailed {
		return "passed"
	}
	return "failed"
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: synora-cge-validation {list|run <scenario-id>|run-all|qualify [--full]|benchmark|campaign {list|run <profile-id>|run-all}} [--json] [--output path] [--events-output path] [--days n] [--full] [--keep-data]")
}
