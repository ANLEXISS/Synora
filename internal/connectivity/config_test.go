package connectivity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigIsDisabledAndSafe(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.Security.ExposeLAN || cfg.Security.AllowIPForwarding {
		t.Fatalf("unsafe defaults: %+v", cfg)
	}
}

func TestConfigRejectsUnknownAndUnsafeValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connectivity.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nenabled: false\nunknown: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field rejection")
	}
	if err := os.WriteFile(path, []byte("version: 1\ninterface:\n  name: synora0\n  listen_port: 41641\n  mtu: 1280\ncontrol:\n  heartbeat_seconds: 30\n  reconnect_min_seconds: 2\n  reconnect_max_seconds: 60\nconnection:\n  direct_timeout_seconds: 8\n  keepalive_seconds: 25\nsecurity:\n  expose_lan: true\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unsafe option rejection")
	}
}

func TestConfigRejectsInvalidPortMTUAndDurations(t *testing.T) {
	base := DefaultConfig()
	for name, mutate := range map[string]func(*Config){
		"port":     func(c *Config) { c.Interface.ListenPort = 0 },
		"mtu":      func(c *Config) { c.Interface.MTU = 100 },
		"duration": func(c *Config) { c.Control.ReconnectMaxSeconds = 1 },
	} {
		cfg := base
		mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("%s should be rejected", name)
		}
	}
}
