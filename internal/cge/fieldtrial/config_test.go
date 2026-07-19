package fieldtrial

import "testing"

func TestLoadConfigExplicitEnvironment(t *testing.T) {
	values := map[string]string{
		"SYNORA_CGE_FIELD_TRIAL_ENABLED":         "true",
		"SYNORA_CGE_FIELD_TRIAL_ROOT":            "/tmp/cge-field-trial-test",
		"SYNORA_CGE_FIELD_TRIAL_SESSION_ID":      "cge-trial-env",
		"SYNORA_CGE_FIELD_TRIAL_SYNC_EACH_EVENT": "true",
		"SYNORA_CGE_FIELD_TRIAL_TOPOLOGY_FILE":   "/tmp/topology.json",
	}
	config, err := LoadConfig(func(key string) string { return values[key] })
	if err != nil {
		t.Fatal(err)
	}
	if !config.Enabled || !config.SyncEachEvent || config.SessionID != "cge-trial-env" || config.TopologyFile != "/tmp/topology.json" {
		t.Fatalf("config=%+v", config)
	}
}

func TestDisabledConfigDoesNotRequireRuntimePaths(t *testing.T) {
	config := DefaultConfig()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
}
