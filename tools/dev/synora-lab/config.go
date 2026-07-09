package main

import (
	"flag"
	"os"
	"strings"

	"synora/pkg/contract"
)

func parseConfig(args []string) (Config, error) {
	cfg := Config{
		BusPath:    defaultBusPath,
		APIURL:     defaultAPIURL,
		Token:      os.Getenv("SYNORA_API_TOKEN"),
		DeviceID:   defaultDevice,
		CameraID:   defaultDevice,
		Identity:   defaultIdentity,
		Confidence: defaultConfidence,
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
