// Command synora-boot-healthcheck performs post-boot checks without changing
// services, pairing state, security mode, runtime state, or configuration.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"synora/internal/boothealth"
	"synora/internal/discovery/network"
	"synora/internal/modelmanifest"
)

const (
	defaultBaseURL  = "http://127.0.0.1:8080"
	defaultManifest = "/opt/synora/models-manifest.yaml"
	defaultModels   = "/var/lib/synora/models"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "explain":
		explain()
	case "run":
		os.Exit(run(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: synora-boot-healthcheck run [--readonly] [--base-url URL] [--report PATH] [--timeout DURATION] | explain")
}

func explain() {
	fmt.Println("boot-readonly checks services, health/state/version endpoints, persistent paths, config readability, driver/network policy, models and recent fatal logs.")
	fmt.Println("It never injects events, opens pairing, changes security mode, runs actions, restarts services, or writes state. Only --report is written.")
	fmt.Println("Exit 0 permits mark-good (including accepted degraded warnings); exit 1 recommends rollback; exit 2 means invalid usage/configuration.")
}

type options struct {
	baseURL, report, manifest, models string
	timeout                           time.Duration
	readonly                          bool
	allowSynoraNetDegraded            bool
}

func run(args []string) int {
	set := flag.NewFlagSet("run", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	opts := options{}
	set.StringVar(&opts.baseURL, "base-url", envOr("SYNORA_BOOT_BASE_URL", defaultBaseURL), "Synora API base URL")
	set.StringVar(&opts.report, "report", "", "JSON report path; no report is written when empty")
	set.StringVar(&opts.manifest, "manifest", envOr("SYNORA_MODELS_MANIFEST", defaultManifest), "model manifest path")
	set.StringVar(&opts.models, "models", envOr("SYNORA_MODELS_ROOT", defaultModels), "model directory")
	set.DurationVar(&opts.timeout, "timeout", 60*time.Second, "overall healthcheck timeout")
	set.BoolVar(&opts.readonly, "readonly", true, "explicitly require readonly checks")
	set.BoolVar(&opts.allowSynoraNetDegraded, "allow-synoranet-degraded", false, "allow degraded SynoraNet without rollback")
	if err := set.Parse(args); err != nil {
		return 2
	}
	if !opts.readonly || strings.TrimSpace(opts.baseURL) == "" || opts.timeout <= 0 {
		return 2
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	checks := runChecks(ctx, opts)
	status, fatalReasons, degradedReasons := boothealth.Aggregate(checks)
	report := boothealth.Report{Status: status, CheckedAt: started.UTC().Format(time.RFC3339), DurationMS: time.Since(started).Milliseconds(), Checks: checks, FatalReasons: fatalReasons, DegradedReasons: degradedReasons}
	if opts.report != "" {
		if err := writeReport(opts.report, report); err != nil {
			fmt.Fprintln(os.Stderr, "cannot write report")
			return 2
		}
	}
	fmt.Printf("boot health: %s (%d checks)\n", status, len(checks))
	return boothealth.ExitCode(status)
}

func runChecks(ctx context.Context, opts options) []boothealth.Check {
	checks := make([]boothealth.Check, 0, 20)
	add := func(name, status, message string, fatal bool) {
		checks = append(checks, boothealth.Check{Name: name, Status: status, Message: message, Fatal: fatal})
	}
	for _, service := range []string{"synora-bus", "synora-core", "synora-api", "synora-discovery", "synora-actions", "synora-connect"} {
		if err := command(ctx, "systemctl", "is-active", "--quiet", service+".service"); err != nil {
			add("service."+service, "fatal", "service is not active", true)
		} else {
			add("service."+service, "ok", "active", false)
		}
	}
	for _, endpoint := range []string{"/api/system/health", "/api/state", "/api/system/version"} {
		status, message := getEndpoint(ctx, opts.baseURL, endpoint)
		add("http."+strings.TrimPrefix(endpoint, "/api/"), status, message, status == "fatal")
	}
	addPath := func(name, path string, writable bool) {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			add(name, "fatal", "required path unavailable", true)
			return
		}
		if writable && syscall.Access(path, 2) != nil {
			add(name, "fatal", "persistent path is not writable", true)
			return
		}
		add(name, "ok", "available", false)
	}
	addPath("path.etc_synora", "/etc/synora", false)
	addPath("path.var_lib_synora", "/var/lib/synora", true)
	modelsRoot := opts.models
	if info, err := os.Stat("/models"); err == nil && info.IsDir() {
		if entries, readErr := os.ReadDir("/models"); readErr == nil && len(entries) > 0 {
			modelsRoot = "/models"
		}
	}
	addPath("path.models", modelsRoot, false)
	if _, err := os.Stat("/etc/synora/security.yaml"); err != nil {
		add("config.security", "fatal", "security.yaml unavailable", true)
	} else {
		add("config.security", "ok", "present", false)
	}
	if _, err := os.Stat("/etc/synora/network.yaml"); err != nil {
		add("config.network", "degraded", "network.yaml unavailable; defaults may apply", false)
	} else if _, err := network.LoadConfig("/etc/synora/network.yaml"); err != nil {
		add("config.network", "fatal", "network.yaml is invalid", true)
	} else {
		add("config.network", "ok", "valid", false)
	}
	manifest, manifestErr := modelmanifest.Load(opts.manifest)
	if manifestErr != nil {
		add("models.manifest", "fatal", "model manifest is unavailable or invalid", true)
		manifest = modelmanifest.Default()
	} else {
		add("models.manifest", "ok", "valid", false)
	}
	if checks := manifest.Check(modelsRoot); len(checks) == 0 {
		add("models.manifest", "fatal", "no model requirements", true)
	} else {
		for _, check := range checks {
			add("model."+check.Name, check.Status, check.Component, check.Required && check.Status == "fatal")
		}
	}
	networkCfg, networkErr := network.LoadConfig("/etc/synora/network.yaml")
	if networkErr == nil && networkCfg.SynoraNet.Enabled {
		if !wifiDriverPresent(ctx) {
			add("network.wifi_driver", "fatal", "SynoraNet enabled but Wi-Fi driver is unavailable", true)
		} else {
			add("network.wifi_driver", "ok", "driver present", false)
		}
		networkStatus, message := getNetworkHealth(ctx, opts.baseURL)
		networkFatal := networkStatus == "fatal" || (networkStatus == "degraded" && !opts.allowSynoraNetDegraded)
		add("network.synoranet", networkStatus, message, networkFatal)
	} else {
		add("network.synoranet", "degraded", "SynoraNet disabled by policy", false)
	}
	if hasRecentFatalLogs(ctx) {
		add("logs.recent_fatal", "fatal", "recent panic or fatal log pattern found", true)
	} else {
		add("logs.recent_fatal", "ok", "no recent panic/fatal pattern", false)
	}
	return checks
}

func getEndpoint(ctx context.Context, baseURL, endpoint string) (string, string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+endpoint, nil)
	if err != nil {
		return "fatal", "invalid API URL"
	}
	if token := strings.TrimSpace(os.Getenv("SYNORA_API_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return "fatal", "API request failed"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "fatal", "API returned unexpected status"
	}
	var payload any
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&payload); err != nil {
		return "fatal", "API returned invalid JSON"
	}
	return "ok", "HTTP 200"
}

func getNetworkHealth(ctx context.Context, baseURL string) (string, string) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/system/health", nil)
	if err != nil {
		return "fatal", "invalid API URL"
	}
	if token := strings.TrimSpace(os.Getenv("SYNORA_API_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	if err != nil {
		return "fatal", "SynoraNet health unavailable"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "fatal", "SynoraNet health unavailable"
	}
	var payload struct {
		Network struct {
			Status    string `json:"status"`
			SynoraNet struct {
				Status string `json:"status"`
			} `json:"synoranet"`
		} `json:"network"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&payload); err != nil {
		return "fatal", "SynoraNet health is invalid"
	}
	status := strings.ToLower(strings.TrimSpace(payload.Network.SynoraNet.Status))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(payload.Network.Status))
	}
	if status == "ok" || status == "degraded" {
		return status, "SynoraNet status=" + status
	}
	return "fatal", "SynoraNet health is unavailable"
}

func command(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

func wifiDriverPresent(ctx context.Context) bool {
	for _, module := range []string{"rtw89_8852be", "rtw89_core", "brcmfmac"} {
		if _, err := os.Stat("/sys/module/" + module); err == nil {
			return true
		}
		if err := command(ctx, "modinfo", module); err == nil {
			return true
		}
	}
	return false
}

func hasRecentFatalLogs(ctx context.Context) bool {
	if _, err := exec.LookPath("journalctl"); err != nil {
		return false
	}
	args := []string{"--since", "-10 minutes", "-u", "synora-bus.service", "-u", "synora-core.service", "-u", "synora-api.service", "-u", "synora-discovery.service", "-u", "synora-actions.service", "--no-pager", "--output", "cat"}
	output, err := exec.CommandContext(ctx, "journalctl", args...).Output()
	if err != nil {
		return false
	}
	text := strings.ToLower(string(output))
	for _, pattern := range []string{"panic", "fatal", "deadlock", "nil pointer", "kernel oops"} {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

func writeReport(path string, report boothealth.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".boot-healthcheck-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0640); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
