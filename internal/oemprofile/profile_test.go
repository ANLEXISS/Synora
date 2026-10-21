package oemprofile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedProfilesStrictlyLoad(t *testing.T) {
	for _, path := range []string{"../../configs/oem/synora-rk3588-central-v1.yaml", "../../configs/oem/synora-generic-cpu-development-v1.yaml"} {
		p, _, err := Load(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if err := p.Validate(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUnknownFieldAndUnsafeCapabilityRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	body, err := os.ReadFile("../../configs/oem/synora-generic-cpu-development-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(body), "profile_id:", "private_key: secret\nprofile_id:", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("unknown field accepted")
	}
	p, _, err := Load("../../configs/oem/synora-generic-cpu-development-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	p.SafetyDefaults.PhysicalActions = true
	if p.Validate() == nil {
		t.Fatal("physical action profile accepted")
	}
}

func TestPreflightSeparatesRequestedObservedEffectiveAndQualification(t *testing.T) {
	p, body, err := Load("../../configs/oem/synora-rk3588-central-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := Preflight(p, body, RuntimeObservation{Architecture: "arm64", RuntimeVersion: "1", StorageWritable: true, APIAuthConfigured: true, RKNNAvailable: false})
	if r.RequestedCapabilities["rknn"] != "prepared" || r.ObservedCapabilities["vision-pose.rk3588-rknn"] != "not_implemented" || r.EffectiveCapabilities["vision-pose"] != "unavailable" {
		t.Fatalf("bad capability separation: %+v", r)
	}
	if r.EffectiveCapabilities["vision-pose"] == "qualified" {
		t.Fatal("profile promoted itself to qualified")
	}
}

func TestProfilePathTraversalRejected(t *testing.T) {
	for _, p := range []string{"../secret.yaml", "/etc/passwd", "configs/oem/../../secret"} {
		if _, err := ResolveProfilePath(p); err == nil {
			t.Fatalf("accepted %q", p)
		}
	}
}

func TestOEMProfileSuiteHasAllRequiredCases(t *testing.T) {
	got := RunScenarios("../../configs/oem/synora-rk3588-central-v1.yaml", "../../configs/oem/synora-generic-cpu-development-v1.yaml", ObserveHost())
	want := []string{"rk3588_valid", "generic_cpu_valid", "unknown_field_rejected", "schema_incompatible", "architecture_incompatible", "adapter_missing", "module_unavailable", "qualified_claim_refused", "model_manifest_hash_invalid", "physical_action_denied", "secret_field_rejected", "path_traversal_rejected", "api_redacted_authenticated", "same_profile_restart_stable", "incompatible_profile_no_store_corruption", "capability_degraded", "discovery_core_mlp_gate_dry_run", "privacy_audio_network_physical_guard"}
	if len(got) != len(want) {
		t.Fatalf("got %d OEM cases, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.ID != want[i] {
			t.Errorf("case %d=%s want %s", i, c.ID, want[i])
		}
		if !c.Passed {
			t.Errorf("%s failed: expected %s actual %s", c.ID, c.Expected, c.Actual)
		}
		if c.AudioRendered || c.PhysicalAction || c.NetworkAccess || c.RawDataForwarded {
			t.Errorf("unsafe behavior in %s", c.ID)
		}
	}
}

func TestAdapterRedactionAllowlist(t *testing.T) {
	for _, key := range []string{"frame", "bbox", "crop", "embedding", "identity", "path", "url", "hardware_id", "camera_id", "track_id", "raw_score"} {
		if ValidateRedactedMetric(key) {
			t.Errorf("forbidden metric accepted: %s", key)
		}
	}
	if !ValidateRedactedMetric("aggregate_latency_ms") {
		t.Fatal("safe aggregate rejected")
	}
}

func TestAdapterPortsRejectRawAndPhysicalInputs(t *testing.T) {
	if ValidateAggregateFeatureInput(AggregateFeatureInput{ContractVersion: "synora.aggregate-features/v1", Features: map[string]float64{"embedding": 0.5}}) {
		t.Fatal("raw biometric feature accepted")
	}
	if ValidateActionProposal(ActionProposal{ActionClass: "unlock", Mode: "physical", CorrelationRef: "opaque"}) {
		t.Fatal("physical action proposal accepted")
	}
	if ValidateCommunicationRequest(CommunicationRequest{ContentClass: "status", OutputMode: "tts"}) {
		t.Fatal("real audio output accepted")
	}
	if ValidateIngressMetadata(IngressMetadata{ContractVersion: "synora.camera.input/v1", InputKind: "rtsp", CorrelationRef: "/etc/passwd"}) {
		t.Fatal("path in ingress reference accepted")
	}
	if ValidateRedactedResult(RedactedResult{Status: "available", AggregatedMetrics: map[string]float64{"embedding": 0.2}}) {
		t.Fatal("raw metric leaked")
	}
}

func TestStartupGateRejectsArchitectureAndVersionMismatch(t *testing.T) {
	p, _, err := Load("../../configs/oem/synora-rk3588-central-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStartup(p, "amd64", "1.0.0"); err == nil {
		t.Fatal("incompatible architecture accepted")
	}
	if err := ValidateStartup(p, "arm64", "0.9.9"); err == nil {
		t.Fatal("old runtime accepted")
	}
	if err := ValidateStartup(p, "arm64", "1.0.0"); err != nil {
		t.Fatalf("compatible profile rejected: %v", err)
	}
}

func TestSelectedModelAdaptersFailClosedWithoutRuntimeAndNeverFallback(t *testing.T) {
	rk, body, err := Load("../../configs/oem/synora-rk3588-central-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	preflight := Preflight(rk, body, RuntimeObservation{Architecture: "arm64", RuntimeVersion: "1.0.0", RKNNAvailable: true, StorageWritable: true, APIAuthConfigured: true})
	if got := preflight.EffectiveCapabilities["vision-pose"]; got != "unavailable" {
		t.Fatalf("RKNN profile silently reported effective=%s", got)
	}
	if got := preflight.ObservedCapabilities["vision-pose.rk3588-rknn"]; got != "not_implemented" {
		t.Fatalf("runtime observation=%s", got)
	}
	cpu, body, err := Load("../../configs/oem/synora-generic-cpu-development-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	preflight = Preflight(cpu, body, RuntimeObservation{Architecture: "arm64", RuntimeVersion: "1.0.0", StorageWritable: true, APIAuthConfigured: true})
	if preflight.EffectiveCapabilities["vision-person"] != "simulated_test" || preflight.EffectiveCapabilities["vision-pose"] != "unavailable" {
		t.Fatalf("unexpected test profile effective capabilities: %+v", preflight.EffectiveCapabilities)
	}
}
