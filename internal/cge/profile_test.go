package cge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"synora/pkg/contract"
)

func TestProfileDefaultsAndAtomicUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cge_profile.yaml")
	store := NewProfileStore(path)
	profile, exists, err := store.Load()
	if err != nil || exists || profile.Mode != contract.CgeSecurityBalanced || profile.SignificantInactivityTimeoutSeconds != 30 {
		t.Fatalf("profile defaults = %#v exists=%v err=%v", profile, exists, err)
	}
	assertProfileArraysNotNil(t, profile)
	updated, err := store.Update([]byte(`{"mode":"strict","global_sensitivity":0.8,"significant_inactivity_timeout_seconds":45}`))
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if updated.Mode != contract.CgeSecurityStrict || updated.GlobalSensitivity != 0.8 || updated.SignificantInactivityTimeoutSeconds != 45 {
		t.Fatalf("updated profile = %#v", updated)
	}
	reloaded, exists, err := NewProfileStore(path).Load()
	if err != nil || !exists || reloaded.Mode != contract.CgeSecurityStrict || reloaded.SignificantInactivityTimeoutSeconds != 45 {
		t.Fatalf("reloaded profile = %#v exists=%v err=%v", reloaded, exists, err)
	}
}

func TestProfileYamlMissingArraysAreEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cge_profile.yaml")
	if err := os.WriteFile(path, []byte("security_profile:\n  mode: strict\n"), 0600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
	profile, exists, err := NewProfileStore(path).Load()
	if err != nil || !exists {
		t.Fatalf("load profile = %#v exists=%v err=%v", profile, exists, err)
	}
	assertProfileArraysNotNil(t, profile)
}

func TestProfilePatchMissingAndNullArraysAreEmpty(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "profile.yaml"))
	if _, err := store.Update([]byte(`{"critical_rooms":null,"ignored_motion_rooms":null}`)); err != nil {
		t.Fatalf("update profile with null arrays: %v", err)
	}
	profile := store.Get()
	assertProfileArraysNotNil(t, profile)
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal profile: %v", err)
	}
	if string(data) == "" || string(data) == "null" {
		t.Fatalf("unexpected profile json: %s", data)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("decode profile json: %v", err)
	}
	if output["critical_rooms"] == nil || output["ignored_motion_rooms"] == nil {
		t.Fatalf("profile arrays must not be null: %s", data)
	}

	if _, err := store.Update([]byte(`{"mode":"strict"}`)); err != nil {
		t.Fatalf("update profile without arrays: %v", err)
	}
	assertProfileArraysNotNil(t, store.Get())
}

func TestNormalizeCgeSecurityProfileBoundsAndDefaults(t *testing.T) {
	profile := contract.NormalizeCgeSecurityProfile(contract.CgeSecurityProfile{
		GlobalSensitivity:            2,
		NightSensitivityMultiplier:   -1,
		ArmedSensitivityMultiplier:   8,
		UnknownPersonTolerance:       "invalid",
		MinimumNotifyDangerLevel:     "invalid",
		MinimumAutoActionDangerLevel: "invalid",
		CriticalRooms:                nil,
		IgnoredMotionRooms:           nil,
	})
	if profile.Mode != contract.CgeSecurityBalanced || profile.GlobalSensitivity != 1 || profile.NightSensitivityMultiplier != 0.1 || profile.ArmedSensitivityMultiplier != 5 {
		t.Fatalf("normalized profile bounds = %#v", profile)
	}
	if profile.UnknownPersonTolerance != "medium" || profile.MinimumNotifyDangerLevel != contract.DangerMedium || profile.MinimumAutoActionDangerLevel != contract.DangerHigh {
		t.Fatalf("normalized profile defaults = %#v", profile)
	}
	assertProfileArraysNotNil(t, profile)
}

func TestProfileRejectsInvalidValues(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "profile.yaml"))
	if _, err := store.Update([]byte(`{"mode":"strict","global_sensitivity":2}`)); err == nil {
		t.Fatal("invalid sensitivity should be rejected")
	}
}

func assertProfileArraysNotNil(t *testing.T, profile contract.CgeSecurityProfile) {
	t.Helper()
	if profile.CriticalRooms == nil || profile.IgnoredMotionRooms == nil {
		t.Fatalf("profile arrays must be non-nil: %#v", profile)
	}
}
