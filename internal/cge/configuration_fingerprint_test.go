package cge

import "testing"

func TestCognitiveConfigurationFingerprintIsStable(t *testing.T) {
	config := DefaultShadowConfig()
	first, err := CognitiveConfigurationFingerprintFor(config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CognitiveConfigurationFingerprintFor(config)
	if err != nil {
		t.Fatal(err)
	}
	if first.CombinedFingerprint == "" || first.CombinedFingerprint != second.CombinedFingerprint {
		t.Fatalf("fingerprints differ: %q %q", first.CombinedFingerprint, second.CombinedFingerprint)
	}
}

func TestCognitiveConfigurationFingerprintDetectsPolicyChange(t *testing.T) {
	firstConfig := DefaultShadowConfig()
	first, err := CognitiveConfigurationFingerprintFor(firstConfig)
	if err != nil {
		t.Fatal(err)
	}
	changed := firstConfig
	changed.Context.Timezone = "Europe/Paris"
	second, err := CognitiveConfigurationFingerprintFor(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.CombinedFingerprint == second.CombinedFingerprint {
		t.Fatal("configuration change did not change fingerprint")
	}
}
