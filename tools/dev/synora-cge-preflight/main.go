// Command synora-cge-preflight validates the offline physical-smoke plan.
// It never creates directories, writes configuration, installs binaries, or
// talks to systemd. It is intentionally usable against a simulated root.
package main

import (
	"bufio"
	"debug/elf"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"synora/internal/cge"
	"synora/internal/cge/shadowworkflow"
	"synora/internal/version"
)

const (
	defaultEnvFile  = "deployments/env/synora-core-cge-shadow.env.example"
	defaultCoreUnit = "deployments/systemd/synora-core.service"
)

type preflightOptions struct {
	Root            string
	BinaryDir       string
	VersionManifest string
	ExpectedCommit  string
	ExpectedArch    string
	UnitPath        string
}

func main() {
	mode := flag.String("mode", "config", "preflight mode: config, filesystem, build, systemd, all")
	envPath := flag.String("env", defaultEnvFile, "environment file to validate")
	root := flag.String("root", "", "simulated filesystem root; empty means no filesystem check")
	binaryDir := flag.String("binary-dir", "", "directory containing built ARM64 binaries")
	versionPath := flag.String("version", "", "version.json generated for the build")
	expectedCommit := flag.String("expected-commit", "", "expected Git commit in version.json")
	expectedArch := flag.String("arch", "arm64", "expected ELF architecture")
	unitPath := flag.String("unit", defaultCoreUnit, "Core systemd unit to audit")
	flag.Parse()

	opts := preflightOptions{Root: *root, BinaryDir: *binaryDir, VersionManifest: *versionPath, ExpectedCommit: *expectedCommit, ExpectedArch: *expectedArch, UnitPath: *unitPath}
	if err := runPreflight(*mode, *envPath, opts); err != nil {
		fmt.Fprintln(os.Stderr, "preflight: FAIL:", err)
		os.Exit(1)
	}
	fmt.Println("preflight: PASS", *mode)
}

func runPreflight(mode, envPath string, opts preflightOptions) error {
	switch mode {
	case "config":
		_, err := validateEnvironmentFile(envPath)
		return err
	case "filesystem":
		return validateFilesystem(opts.Root)
	case "build":
		return validateBuild(opts)
	case "systemd":
		return validateSystemdUnit(opts.UnitPath)
	case "all":
		if _, err := validateEnvironmentFile(envPath); err != nil {
			return err
		}
		if err := validateSystemdUnit(opts.UnitPath); err != nil {
			return err
		}
		if opts.Root == "" || opts.BinaryDir == "" {
			return errors.New("all mode requires --root and --binary-dir")
		}
		if err := validateFilesystem(opts.Root); err != nil {
			return err
		}
		return validateBuild(opts)
	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}

func validateEnvironmentFile(path string) (map[string]string, error) {
	values, err := parseEnvironmentFile(path)
	if err != nil {
		return nil, err
	}
	for key, value := range values {
		if !recognizedEnvironment[key] {
			return nil, fmt.Errorf("environment variable %s is not parsed by production", key)
		}
		if secretLike(key) || secretLike(value) {
			return nil, fmt.Errorf("secret-like environment entry %s is forbidden", key)
		}
	}
	config, err := cge.LoadShadowConfig(func(key string) string { return values[key] })
	if err != nil {
		return nil, fmt.Errorf("load Shadow environment: %w", err)
	}
	if !config.Enabled || !config.Workflow.Enabled || !config.Context.Enabled || !config.Workflow.CalibrationLedger.Enabled || !config.Workflow.CalibrationAnalytics.Enabled {
		return nil, errors.New("profile does not enable Shadow, workflow, live context, ledger, and analytics")
	}
	if config.Cognitive.Enabled || config.Cognitive.AutoApplyDecisiveEvidence || config.Routines.Enabled || config.Deviation.Enabled || config.Workflow.Qualification.Enabled || config.FieldTrial.Enabled {
		return nil, errors.New("profile enables a cognitive authority, learning, qualification, or field trial path")
	}
	if !config.Workflow.CalibrationLedger.Fsync || config.Workflow.CalibrationLedger.MaxBytes != 16777216 || config.Workflow.CalibrationLedger.MaxRecords != 1000 {
		return nil, errors.New("profile does not use the bounded fsync ledger smoke limits")
	}
	if !reflect.DeepEqual(config.EligibleEventTypes, cge.DefaultEligibleEventTypes()) {
		return nil, errors.New("profile changed the default Shadow admission set")
	}
	if config.DataDir != "/var/lib/synora/cge" || config.JournalPath != "/var/lib/synora/cge/journal.ndjson" || config.Workflow.CalibrationLedger.Path != "/var/lib/synora/cge/calibration-ledger.ndjson" {
		return nil, errors.New("profile uses unexpected durable paths")
	}
	return values, nil
}

func parseEnvironmentFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open environment file: %w", err)
	}
	defer file.Close()
	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t\r\n") || strings.HasPrefix(key, "export ") {
			return nil, fmt.Errorf("invalid environment syntax at line %d", line)
		}
		if _, exists := values[key]; exists {
			return nil, fmt.Errorf("duplicate environment variable %s", key)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("empty environment value for %s", key)
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read environment file: %w", err)
	}
	return values, nil
}

func validateFilesystem(root string) error {
	if strings.TrimSpace(root) == "" || !filepath.IsAbs(root) {
		return errors.New("filesystem mode requires an absolute --root")
	}
	requiredDirs := []string{"/opt/synora/bin", "/var/lib/synora", "/var/lib/synora/cge", "/etc/synora", "/run/synora"}
	for _, path := range requiredDirs {
		if err := checkPath(root, path, true); err != nil {
			return err
		}
	}
	for _, path := range []string{"/opt/synora/bin/synora-core", "/opt/synora/bin/synora-bus", "/opt/synora/bin/synora-actions", "/opt/synora/version.json"} {
		if err := checkPath(root, path, false); err != nil {
			return err
		}
	}
	return nil
}

func checkPath(root, target string, directory bool) error {
	path := filepath.Join(root, strings.TrimPrefix(target, string(filepath.Separator)))
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("required %s missing at %s: %w", target, path, err)
	}
	if info.IsDir() != directory {
		return fmt.Errorf("path type mismatch for %s", target)
	}
	mode := info.Mode().Perm()
	if mode&0002 != 0 {
		return fmt.Errorf("path %s is world-writable", target)
	}
	if !directory && strings.HasPrefix(target, "/opt/synora/bin/") && mode&0100 == 0 {
		return fmt.Errorf("binary %s is not owner-executable", target)
	}
	return nil
}

func validateBuild(opts preflightOptions) error {
	if strings.TrimSpace(opts.BinaryDir) == "" || !filepath.IsAbs(opts.BinaryDir) {
		return errors.New("build mode requires an absolute --binary-dir")
	}
	expectedMachine := elf.EM_AARCH64
	if opts.ExpectedArch != "" && opts.ExpectedArch != "arm64" {
		return fmt.Errorf("unsupported expected architecture %q", opts.ExpectedArch)
	}
	for _, name := range []string{"synora-core", "synora-bus", "synora-actions"} {
		path := filepath.Join(opts.BinaryDir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode().Perm()&0100 == 0 {
			return fmt.Errorf("built executable %s is missing or not executable", path)
		}
		file, err := elf.Open(path)
		if err != nil {
			return fmt.Errorf("inspect ELF %s: %w", path, err)
		}
		machine := file.Machine
		_ = file.Close()
		if machine != expectedMachine {
			return fmt.Errorf("binary %s has ELF machine %s, want AARCH64", path, machine)
		}
	}
	if opts.VersionManifest != "" {
		data, err := os.ReadFile(opts.VersionManifest)
		if err != nil {
			return fmt.Errorf("read version manifest: %w", err)
		}
		var manifest version.Manifest
		if err := json.Unmarshal(data, &manifest); err != nil || manifest.ConfigSchemaVersion <= 0 {
			return errors.New("invalid version manifest")
		}
		if opts.ExpectedCommit != "" && manifest.GitCommit != opts.ExpectedCommit && !strings.HasPrefix(opts.ExpectedCommit, manifest.GitCommit) {
			return fmt.Errorf("version manifest commit %q does not match %q", manifest.GitCommit, opts.ExpectedCommit)
		}
	}
	return nil
}

func validateSystemdUnit(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Core systemd unit: %w", err)
	}
	text := string(data)
	for _, required := range []string{
		"User=synora", "Group=synora", "ExecStart=/opt/synora/bin/synora-core",
		"EnvironmentFile=-/etc/synora/synora-core.env", "Requires=synora-bus.service",
		"Restart=always", "NoNewPrivileges=true",
	} {
		if !strings.Contains(text, required) {
			return fmt.Errorf("Core unit missing %s", required)
		}
	}
	return nil
}

func secretLike(value string) bool {
	value = strings.ToUpper(value)
	for _, marker := range []string{"TOKEN", "PASSWORD", "SECRET", "PRIVATE_KEY", "PSK", "CREDENTIAL"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

var recognizedEnvironment = func() map[string]bool {
	values := []string{
		cge.ShadowEnabledEnv, cge.ShadowDataDirEnv, cge.ShadowJournalPathEnv, cge.ShadowInitializeEnv, cge.ShadowJournalIDEnv,
		cge.ShadowCognitiveEnabledEnv, cge.ShadowAutoEvidenceEnv, cge.ShadowMaxReevaluationsEnv,
		cge.ShadowContextEnabledEnv, cge.ShadowContextTimezoneEnv, cge.ShadowContextAllowPartialEnv,
		cge.ShadowRoutineLearningEnabledEnv, cge.ShadowRoutineBucketEnv, cge.ShadowRoutineAllowPartialEnv,
		cge.ShadowRoutineMaxGapEnv, cge.ShadowRoutineSameTopologyEnv, cge.ShadowDeviationEnabledEnv,
		cge.ShadowDeviationRecentLimitEnv, cge.ShadowDeviationMaxAssessmentsEnv, cge.ShadowWorkflowEnabledEnv,
		shadowworkflow.CalibrationLedgerEnabledEnv, shadowworkflow.CalibrationLedgerPathEnv, shadowworkflow.CalibrationLedgerFsyncEnv,
		shadowworkflow.CalibrationLedgerMaxBytesEnv, shadowworkflow.CalibrationLedgerMaxRecordsEnv, shadowworkflow.CalibrationLedgerRepairTrailingEnv,
		shadowworkflow.CalibrationAnalyticsEnabledEnv, shadowworkflow.CalibrationAnalyticsMinRecordsEnv, shadowworkflow.CalibrationAnalyticsMinComparableEnv,
		shadowworkflow.CalibrationAnalyticsWindowSizeEnv, shadowworkflow.CalibrationAnalyticsMaxWindowsEnv,
		shadowworkflow.QualificationEnabledEnv, shadowworkflow.QualificationRunIDEnv, shadowworkflow.QualificationProfileEnv,
		shadowworkflow.QualificationOutputDirEnv, shadowworkflow.QualificationSampleIntervalEnv, shadowworkflow.QualificationMaxOutputBytesEnv,
		"SYNORA_CGE_FIELD_TRIAL_ENABLED", "SYNORA_CGE_FIELD_TRIAL_ROOT", "SYNORA_CGE_FIELD_TRIAL_SESSION_ID",
		"SYNORA_CGE_FIELD_TRIAL_SEGMENT_MAX_BYTES", "SYNORA_CGE_FIELD_TRIAL_RETENTION_DAYS", "SYNORA_CGE_FIELD_TRIAL_MAX_TOTAL_BYTES",
		"SYNORA_CGE_FIELD_TRIAL_SYNC_EACH_EVENT", "SYNORA_CGE_FIELD_TRIAL_KEY_FILE", "SYNORA_CGE_FIELD_TRIAL_TOPOLOGY_FILE",
		"SYNORA_CGE_FIELD_TRIAL_INCLUDE_CONTEXT", "SYNORA_CGE_FIELD_TRIAL_INCLUDE_LATENCIES",
	}
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}()

func sortedRecognizedEnvironment() []string {
	result := make([]string, 0, len(recognizedEnvironment))
	for key := range recognizedEnvironment {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
