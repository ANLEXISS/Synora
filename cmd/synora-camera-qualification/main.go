// Command synora-camera-qualification creates a read-only, redacted report
// of the camera prerequisites needed before J2 can be validated. It never
// pairs, writes device configuration, changes MediaMTX, or opens a stream.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"synora/internal/device"
	"synora/internal/mediamtx"
	"synora/internal/runtimeconfig"
	"synora/internal/security"
	"synora/pkg/contract"
)

type cameraReport struct {
	ID               string   `json:"id"`
	Enabled          bool     `json:"enabled"`
	Trusted          bool     `json:"trusted"`
	NetworkTrust     string   `json:"network_trust,omitempty"`
	Endpoint         string   `json:"endpoint,omitempty"`
	Capabilities     []string `json:"capabilities,omitempty"`
	IdentityPresent  bool     `json:"identity_present"`
	IdentityStatus   string   `json:"identity_status"`
	MediaPathPresent bool     `json:"media_path_present"`
	Qualification    string   `json:"qualification"`
	QualificationWhy []string `json:"qualification_reasons,omitempty"`
}

type qualificationReport struct {
	SchemaVersion      string         `json:"schema_version"`
	CheckedAt          time.Time      `json:"checked_at"`
	Decision           string         `json:"decision"`
	Passed             bool           `json:"passed"`
	ConfigPath         string         `json:"config_path"`
	IdentityRegistry   string         `json:"identity_registry"`
	MediaMTXAPI        string         `json:"mediamtx_api"`
	MediaMTXPathNames  []string       `json:"mediamtx_active_paths"`
	DiscoveryHealthURL string         `json:"discovery_health_url"`
	DiscoveryHealth    map[string]any `json:"discovery_health,omitempty"`
	Cameras            []cameraReport `json:"cameras"`
	Reasons            []string       `json:"reasons,omitempty"`
}

func main() {
	out := flag.String("out", "/tmp/synora-camera-qualification.json", "redacted JSON report path")
	flag.Parse()
	runtime, err := runtimeconfig.Load(os.Getenv)
	if err != nil {
		fatalReport(*out, qualificationReport{SchemaVersion: "synora.camera-qualification/v1", CheckedAt: time.Now().UTC(), Decision: "unknown", Reasons: []string{"runtime configuration unavailable"}})
	}
	report := qualify(runtime)
	if err := writeReport(*out, report); err != nil {
		fmt.Fprintln(os.Stderr, "write qualification report:", err)
		os.Exit(1)
	}
	fmt.Printf("camera qualification report=%s decision=%s cameras=%d active_paths=%d\n", *out, report.Decision, len(report.Cameras), len(report.MediaMTXPathNames))
	if !report.Passed {
		os.Exit(1)
	}
}

func qualify(runtime runtimeconfig.Config) qualificationReport {
	report := qualificationReport{
		SchemaVersion: "synora.camera-qualification/v1",
		CheckedAt:     time.Now().UTC(), ConfigPath: runtime.Paths.Devices,
		IdentityRegistry:   runtime.Paths.IdentityRegistry,
		MediaMTXAPI:        runtime.Endpoints.MediaMTXAPIURL,
		DiscoveryHealthURL: discoveryHealthURL(runtime.Endpoints.VisionHealth),
		Decision:           "not_qualified",
	}
	configs, err := device.Load(runtime.Paths.Devices)
	if err != nil {
		report.Reasons = append(report.Reasons, "camera configuration unavailable")
		return report
	}
	identities := security.NewIdentityRegistry(runtime.Paths.IdentityRegistry)
	identityErr := identities.Load()
	if identityErr != nil {
		report.Reasons = append(report.Reasons, "camera identity registry unavailable")
	}
	paths, pathErr := listMediaPaths(runtime.Endpoints.MediaMTXAPIURL)
	if pathErr != nil {
		report.Reasons = append(report.Reasons, "MediaMTX path inventory unavailable")
	}
	pathSet := make(map[string]bool, len(paths))
	for _, path := range paths {
		pathSet[path] = true
	}
	report.MediaMTXPathNames = paths
	report.DiscoveryHealth = fetchHealth(report.DiscoveryHealthURL)
	for _, configured := range configs {
		if configured.Type != contract.DeviceTypeCamera || configured.DeletedAt != nil {
			continue
		}
		item := cameraReport{ID: configured.ID, Enabled: configured.Enabled, Trusted: configured.Trusted, Capabilities: append([]string(nil), configured.Capabilities...), Qualification: "not_qualified"}
		item.NetworkTrust = stringValue(configured.Network, "network_trust")
		item.Endpoint = firstNetworkValue(configured.Network, "rtsp_url", "rtsp", "endpoint", "url", "ip", "static_ip")
		if identityErr == nil {
			identity, ok := identities.Lookup(configured.ID)
			item.IdentityPresent = ok
			if ok {
				item.IdentityStatus = string(identity.Status)
			}
		}
		item.MediaPathPresent = pathSet[configured.ID]
		if !item.Enabled {
			item.QualificationWhy = append(item.QualificationWhy, "camera disabled")
		}
		if !item.Trusted {
			item.QualificationWhy = append(item.QualificationWhy, "camera not trusted")
		}
		if item.NetworkTrust != "paired" {
			item.QualificationWhy = append(item.QualificationWhy, "network trust is not paired")
		}
		if !item.IdentityPresent || item.IdentityStatus != string(security.IdentityActive) {
			item.QualificationWhy = append(item.QualificationWhy, "active camera identity missing")
		}
		if item.Endpoint == "" {
			item.QualificationWhy = append(item.QualificationWhy, "camera endpoint missing")
		}
		if !item.MediaPathPresent {
			item.QualificationWhy = append(item.QualificationWhy, "no active MediaMTX path")
		}
		if len(item.QualificationWhy) == 0 {
			item.Qualification = "transport_qualified"
		}
		report.Cameras = append(report.Cameras, item)
	}
	if len(report.Cameras) == 0 {
		report.Reasons = append(report.Reasons, "no configured camera")
		return report
	}
	for _, camera := range report.Cameras {
		if camera.Qualification != "transport_qualified" {
			report.Reasons = append(report.Reasons, camera.ID+": "+strings.Join(camera.QualificationWhy, ", "))
			continue
		}
		report.Passed = true
	}
	if report.Passed {
		report.Decision = "transport_qualified_pending_pipeline"
	}
	return report
}

func listMediaPaths(rawURL string) ([]string, error) {
	client, err := mediamtx.NewClient(rawURL, &http.Client{Timeout: 2 * time.Second})
	if err != nil {
		return nil, err
	}
	return client.ListPaths(context.Background())
}

func fetchHealth(rawURL string) map[string]any {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	response, err := (&http.Client{Timeout: 2 * time.Second}).Get(rawURL)
	if err != nil {
		return map[string]any{"status": "unknown"}
	}
	defer response.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return map[string]any{"status": "unknown"}
	}
	if response.StatusCode >= 400 {
		payload["http_status"] = response.StatusCode
	}
	return payload
}

func discoveryHealthURL(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	if strings.HasPrefix(address, "http://") || strings.HasPrefix(address, "https://") {
		return strings.TrimRight(address, "/") + "/healthz"
	}
	if strings.HasPrefix(address, ":") {
		return "http://127.0.0.1" + address + "/healthz"
	}
	return "http://" + address + "/healthz"
}

func firstNetworkValue(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(values, key); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func writeReport(path string, report qualificationReport) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}

func fatalReport(path string, report qualificationReport) {
	if err := writeReport(path, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	fmt.Printf("camera qualification report=%s decision=%s\n", path, report.Decision)
	os.Exit(1)
}
