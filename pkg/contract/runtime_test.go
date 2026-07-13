package contract

import (
	"testing"
	"time"
)

func TestNormalizeRuntimeHealthFillsUnavailableComponents(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	health := NormalizeRuntimeHealth(RuntimeHealth{}, now)
	if health.Disk.Path == "" || health.Network.HostAPD.Name == "" || health.Network.DNSMasq.Name == "" {
		t.Fatalf("health=%#v", health)
	}
	for _, name := range []string{"synora-core", "synora-actions", "synora-discovery", "mediamtx"} {
		item := health.Services[name]
		if item.Name == "" || item.Checked.IsZero() || item.Status == "" {
			t.Fatalf("service %s=%#v", name, item)
		}
	}
	if health.Components["actions"].Status == "" || health.Components["vision_worker"].Status == "" {
		t.Fatalf("components=%#v", health.Components)
	}
}
