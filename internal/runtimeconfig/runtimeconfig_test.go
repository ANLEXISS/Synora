package runtimeconfig

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadUsesProductionDefaults(t *testing.T) {
	cfg, err := Load(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Paths.BusSocket != DefaultBusSocket || cfg.Paths.State != DefaultStatePath || cfg.Endpoints.HTTP != DefaultHTTPAddr || cfg.Endpoints.MediaMTXRTSPURL != DefaultMediaMTXRTSPURL || cfg.Endpoints.MediaMTXAPIURL != "http://127.0.0.1:9997" || cfg.Timeouts.BusRPC != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadAcceptsInjectedHermeticPathsAndEphemeralPorts(t *testing.T) {
	root := t.TempDir()
	values := map[string]string{
		"SYNORA_CONFIG_DIR":         root,
		"SYNORA_BUS":                filepath.Join(root, "bus.sock"),
		"SYNORA_STATE_PATH":         filepath.Join(root, "state.json"),
		"SYNORA_CLIP_ROOT":          filepath.Join(root, "clips"),
		"SYNORA_FACE_DATA_ROOT":     filepath.Join(root, "faces"),
		"SYNORA_MODEL_ROOT":         filepath.Join(root, "models"),
		"SYNORA_BACKUP_ROOT":        filepath.Join(root, "backups"),
		"SYNORA_WEB_ROOT":           filepath.Join(root, "web"),
		"SYNORA_HTTP_ADDR":          "127.0.0.1:0",
		"SYNORA_HTTPS_ADDR":         "127.0.0.1:0",
		"SYNORA_VISION_HEALTH_ADDR": "127.0.0.1:0",
		"SYNORA_VISION_HTTPS_ADDR":  "127.0.0.1:0",
		"SYNORA_BUS_RPC_TIMEOUT":    "250ms",
		"SYNORA_MEDIAMTX_API_URL":   "http://127.0.0.1:19997",
	}
	cfg, err := Load(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Paths.State != values["SYNORA_STATE_PATH"] || cfg.Endpoints.HTTP != "127.0.0.1:0" || cfg.Endpoints.MediaMTXAPIURL != values["SYNORA_MEDIAMTX_API_URL"] || cfg.Timeouts.BusRPC != 250*time.Millisecond {
		t.Fatalf("injected values were not applied: %+v", cfg)
	}
}

func TestLoadRejectsInvalidConfigurationEarly(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{name: "relative state", key: "SYNORA_STATE_PATH", value: "state.json"},
		{name: "invalid endpoint", key: "SYNORA_HTTP_ADDR", value: "not-an-address"},
		{name: "invalid duration", key: "SYNORA_BUS_RPC_TIMEOUT", value: "soon"},
		{name: "invalid mediamtx URL", key: "SYNORA_MEDIAMTX_RTSP_URL", value: "https://example.test/stream"},
		{name: "invalid mediamtx API URL", key: "SYNORA_MEDIAMTX_API_URL", value: "rtsp://example.test"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(func(key string) string {
				if key == test.key {
					return test.value
				}
				return ""
			})
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "runtime") {
				t.Fatalf("expected early runtime configuration error, got %v", err)
			}
		})
	}
}
