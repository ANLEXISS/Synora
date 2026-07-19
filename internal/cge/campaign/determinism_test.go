package campaign

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestCampaignDurableResultsDeterministic(t *testing.T) {
	profile, _ := ProfileByID("stable_single_resident_30d")
	options := func(root string) RunOptions {
		return RunOptions{RootDir: filepath.Join(root, "campaign"), DaysOverride: 7}
	}
	a, err := Run(context.Background(), profile, options(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Run(context.Background(), profile, options(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	// Wall-clock fields and latency are intentionally excluded; the durable
	// and simulated portions must be byte-for-byte stable.
	a.StartedAt, a.EndedAt = b.StartedAt, b.EndedAt
	a.Latency = b.Latency
	if reflectJSON(a) != reflectJSON(b) {
		t.Fatal("independent runs diverged in deterministic campaign output")
	}
}

func reflectJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
