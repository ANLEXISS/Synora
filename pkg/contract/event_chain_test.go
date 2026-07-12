package contract

import (
	"encoding/json"
	"testing"
)

func TestNormalizeCriticalChainMemoryUsesEmptyArrays(t *testing.T) {
	memory := NormalizeCriticalChainMemory(CriticalChainMemory{})
	data, err := json.Marshal(memory)
	if err != nil {
		t.Fatalf("marshal critical memory: %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("decode critical memory: %v", err)
	}
	for _, key := range []string{
		"recent_chain_ids", "significant_event_types", "node_pattern", "device_types",
		"identity_pattern", "typical_state_path", "typical_danger_path",
		"recommended_actions", "actions_taken", "outcomes",
	} {
		if output[key] == nil {
			t.Fatalf("%s must not be null: %s", key, data)
		}
	}
}
