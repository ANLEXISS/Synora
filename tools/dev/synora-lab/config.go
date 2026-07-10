package main

import (
	"flag"
	"os"
	"strings"

	"synora/pkg/contract"
)

func parseConfig(args []string) (Config, error) {
	cfg := Config{
		BusPath:           defaultBusPath,
		APIURL:            defaultAPIURL,
		Token:             os.Getenv("SYNORA_API_TOKEN"),
		DeviceID:          defaultDevice,
		CameraID:          defaultDevice,
		Identity:          defaultIdentity,
		Confidence:        defaultConfidence,
		ExpectDangerLevel: -1,
		ExpectMinDangerLevel: -1,
		LearningMode:      "simulation",
		Repeat:            1,
	}

	fs := flag.NewFlagSet("synora-lab", flag.ContinueOnError)
	fs.StringVar(&cfg.BusPath, "bus", cfg.BusPath, "Unix bus socket path")
	fs.StringVar(&cfg.APIURL, "api", cfg.APIURL, "Synora API base URL or /api/state URL")
	fs.StringVar(&cfg.Token, "token", cfg.Token, "API bearer token")
	fs.StringVar(&cfg.DeviceID, "device", cfg.DeviceID, "device id to simulate")
	fs.StringVar(&cfg.CameraID, "camera", cfg.CameraID, "camera id to simulate")
	fs.StringVar(&cfg.NodeID, "node", cfg.NodeID, "node id override")
	fs.StringVar(&cfg.Identity, "identity", cfg.Identity, "identity for vision.identity")
	fs.Float64Var(&cfg.Confidence, "confidence", cfg.Confidence, "event confidence")
	fs.StringVar(&cfg.SendType, "send", "", "event type to send")
	fs.StringVar(&cfg.Scenario, "scenario", "", "scenario name to run")
	fs.BoolVar(&cfg.Watch, "watch", false, "watch snapshot without interactive input")
	fs.BoolVar(&cfg.NoTUI, "no-tui", false, "disable interactive terminal UI")
	fs.BoolVar(&cfg.ListScenarios, "list-scenarios", false, "list available simulation scenarios")
	fs.BoolVar(&cfg.DryRunActions, "dry-run-actions", false, "mark simulated events so resulting actions are dry-run")
	fs.BoolVar(&cfg.Verbose, "verbose", false, "print bus/API settings and full event messages")
	fs.BoolVar(&cfg.ShowCGE, "show-cge", false, "print CGE learned sequences and transitions from PublicSnapshot")
	fs.BoolVar(&cfg.ShowDanger, "show-danger", false, "print CGE danger assessments from PublicSnapshot")
	fs.BoolVar(&cfg.ShowDangerAll, "show-danger-all", false, "print all recent CGE danger assessments instead of only the current run")
	fs.BoolVar(&cfg.InspectLearning, "inspect-learning", false, "print CGE learning after scenario execution")
	fs.StringVar(&cfg.ExpectSequence, "expect-sequence", "", "fail unless the named scenario sequence appears in CGE learning")
	fs.IntVar(&cfg.ExpectDangerLevel, "expect-danger-level", cfg.ExpectDangerLevel, "fail unless the latest danger assessment has this level")
	fs.IntVar(&cfg.ExpectMinDangerLevel, "expect-min-danger-level", cfg.ExpectMinDangerLevel, "fail unless the max selected danger assessment level is at least this level")
	fs.StringVar(&cfg.ExpectCategory, "expect-category", "", "fail unless the latest danger assessment has this category")
	fs.StringVar(&cfg.ExpectSystemAction, "expect-system-action", "", "fail unless the latest danger assessment recommends this system action")
	fs.StringVar(&cfg.ExpectSystemState, "expect-system-state", "", "fail unless the system reaches this state")
	fs.BoolVar(&cfg.ExpectEmergencyActive, "expect-emergency-active", false, "fail unless emergency_active becomes true")
	fs.BoolVar(&cfg.ExpectIntrusionActive, "expect-intrusion-active", false, "fail unless intrusion_active becomes true")
	fs.StringVar(&cfg.LearningMode, "learning-mode", cfg.LearningMode, "learning mode for simulated events: simulation or disabled")
	fs.IntVar(&cfg.Repeat, "repeat", cfg.Repeat, "number of times to run a scenario")

	cfg.identityExplicit = flagWasSet(args, "identity")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.APIURL = normalizeStateURL(cfg.APIURL)
	cfg.HealthURL = normalizeHealthURL(cfg.APIURL)
	if cfg.CameraID == "" {
		cfg.CameraID = cfg.DeviceID
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = cfg.CameraID
	}
	cfg.SendType = strings.TrimSpace(cfg.SendType)
	cfg.Scenario = strings.TrimSpace(cfg.Scenario)
	cfg.ExpectSequence = strings.TrimSpace(cfg.ExpectSequence)
	cfg.ExpectCategory = strings.TrimSpace(cfg.ExpectCategory)
	cfg.ExpectSystemAction = strings.TrimSpace(cfg.ExpectSystemAction)
	cfg.ExpectSystemState = strings.TrimSpace(cfg.ExpectSystemState)
	cfg.LearningMode = strings.ToLower(strings.TrimSpace(cfg.LearningMode))
	if cfg.LearningMode == "" {
		cfg.LearningMode = "simulation"
	}
	if cfg.LearningMode != "simulation" && cfg.LearningMode != "disabled" {
		return cfg, flag.ErrHelp
	}
	if cfg.Repeat < 1 {
		cfg.Repeat = 1
	}
	if cfg.SendType == "" && cfg.Scenario == "" && !cfg.Watch && !cfg.NoTUI && !cfg.ListScenarios && cfg.identityExplicit {
		cfg.SendType = contract.EventVisionIdentity
	}
	return cfg, nil
}

func normalizeStateURL(value string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return defaultAPIURL
	}
	if strings.HasSuffix(value, "/api/state") {
		return value
	}
	return value + "/api/state"
}

func normalizeHealthURL(stateURL string) string {
	stateURL = strings.TrimRight(strings.TrimSpace(stateURL), "/")
	if strings.HasSuffix(stateURL, "/api/state") {
		return strings.TrimSuffix(stateURL, "/api/state") + "/api/system/health"
	}
	return strings.TrimRight(stateURL, "/") + "/api/system/health"
}

func flagWasSet(args []string, name string) bool {
	prefix := "--" + name + "="
	exact := "--" + name
	for _, arg := range args {
		if arg == exact || strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}
