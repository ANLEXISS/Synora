package contract

import (
	"strings"
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

func TestNormalizeRuntimeHealthComputesDiskUsagePercent(t *testing.T) {
	health := NormalizeRuntimeHealth(RuntimeHealth{Disk: RuntimeDiskHealth{
		Path: "/var/lib/synora", TotalBytes: 1000, UsedBytes: 250,
	}}, time.Now().UTC())
	if health.Disk.UsedPercent != 25 {
		t.Fatalf("disk=%#v", health.Disk)
	}
}

func TestMergeRuntimeComponentStatusOverridesGenericProbe(t *testing.T) {
	now := time.Now().UTC()
	health := NormalizeRuntimeHealth(RuntimeHealth{}, now)
	merged := MergeRuntimeComponentStatus(health, map[string]string{
		"discovery":      "degraded",
		"vision_worker":  "unavailable",
		"vision_ingress": "disabled",
	}, now)
	if merged.Components["discovery"].Status != "degraded" || merged.Services["synora-discovery"].Status != "degraded" {
		t.Fatalf("discovery mismatch=%#v/%#v", merged.Components["discovery"], merged.Services["synora-discovery"])
	}
	if merged.Components["vision_worker"].Status != "unavailable" || merged.Components["vision_ingress"].Status != "disabled" {
		t.Fatalf("component mismatch=%#v", merged.Components)
	}
}

func TestMergeRuntimeComponentStatusDetailedExplainsDiscoveryDegradation(t *testing.T) {
	now := time.Now().UTC()
	health := RuntimeHealth{
		Network: RuntimeNetworkHealth{HostAPD: RuntimeServiceHealth{
			Name: "hostapd", Status: "degraded", Active: false,
		}},
	}
	merged := MergeRuntimeComponentStatusDetailed(
		health,
		map[string]string{
			"discovery":     "degraded",
			"network":       "degraded",
			"vision_worker": "unavailable",
		},
		map[string]string{"vision_worker": "model_missing"},
		map[string]string{"arcface": "missing"},
		now,
	)
	message := merged.Services["synora-discovery"].Message
	if merged.Components["discovery"].Status != "degraded" || !merged.Components["discovery"].Active {
		t.Fatalf("discovery=%#v", merged.Components["discovery"])
	}
	if !strings.Contains(message, "hostapd") || !strings.Contains(message, "models missing") {
		t.Fatalf("message=%q", message)
	}
}

func TestMergeRuntimeComponentStatusDetailedExplainsIngressMode(t *testing.T) {
	now := time.Now().UTC()
	disabled := MergeRuntimeComponentStatusDetailed(
		RuntimeHealth{},
		map[string]string{"discovery": "degraded", "vision_ingress": "disabled"},
		map[string]string{"vision_ingress": "tls_cert_missing"},
		nil,
		now,
	)
	if disabled.Components["vision_ingress"].Message != "disabled: tls_cert_missing" {
		t.Fatalf("disabled=%#v", disabled.Components["vision_ingress"])
	}
	secure := MergeRuntimeComponentStatusDetailed(
		RuntimeHealth{},
		map[string]string{"vision_ingress": "ok"},
		map[string]string{"vision_ingress": "listening"},
		nil,
		now,
	)
	if secure.Components["vision_ingress"].Message != "listening" {
		t.Fatalf("secure=%#v", secure.Components["vision_ingress"])
	}
}
