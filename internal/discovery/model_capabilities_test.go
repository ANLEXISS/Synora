package discovery

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOEMModelSelectionProducesFailClosedEffectiveDiscoveryState(t *testing.T) {
	t.Setenv("SYNORA_OEM_PROFILE", "../../configs/oem/synora-generic-cpu-development-v1.yaml")
	root := t.TempDir()
	for _, name := range []string{"synora-person-mock-test", "synora-pose-replay-test"} {
		body, err := os.ReadFile(filepath.Join("../../configs/oem/model-packages", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(root, name+".json"), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SYNORA_OEM_PACKAGE_ROOT", root)
	t.Setenv("SYNORA_MODEL_ROOT", "")
	t.Setenv("SYNORA_TEST_VISION_WORKER", "1")
	caps := loadModelCapabilities()
	if caps["vision-person"].EffectiveState != "simulated_test" || caps["vision-pose"].EffectiveState != "unavailable" {
		t.Fatalf("unexpected effective states: %+v", caps)
	}
	if !capabilitiesPermitVision(caps, true) || capabilitiesPermitVision(caps, false) {
		t.Fatal("test-only profile admission did not fail closed")
	}
	for module, c := range caps {
		if c.QualificationStatus == "qualified" || (c.EffectiveState != "not_configured" && (c.ArtifactFormat == "" || c.PackageID == "")) {
			t.Errorf("invalid API capability %s: %+v", module, c)
		}
	}
}

func TestDiscoveryCapabilityHealthIsRedacted(t *testing.T) {
	capability := ModuleRuntimeCapability{ModuleID: "vision-pose", RequestedState: "prepared", ObservedAdapterState: "not_implemented", EffectiveState: "unavailable", PackageID: "opaque-package", ArtifactFormat: "rknn", CompatibilityStatus: "compatible", QualificationStatus: "prepared", ErrorCategory: "runtime_adapter_unavailable"}
	api := (&WebAPI{Health: func() map[string]any {
		return map[string]any{"module_capabilities": map[string]ModuleRuntimeCapability{"vision-pose": capability}}
	}}).Handler()
	r := httptest.NewRecorder()
	api.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	if r.Code != http.StatusOK {
		t.Fatalf("status %d", r.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"artifact_sha256", "path", "tensor", "bbox", "embedding", "label", "device_id"} {
		if strings.Contains(r.Body.String(), forbidden) {
			t.Errorf("forbidden key %q in capability API", forbidden)
		}
	}
}
