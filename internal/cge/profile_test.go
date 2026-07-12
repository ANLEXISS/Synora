package cge

import (
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

func TestProfileRejectsInvalidValues(t *testing.T) {
	store := NewProfileStore(filepath.Join(t.TempDir(), "profile.yaml"))
	if _, err := store.Update([]byte(`{"mode":"strict","global_sensitivity":2}`)); err == nil {
		t.Fatal("invalid sensitivity should be rejected")
	}
}
