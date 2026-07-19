package main

import (
	"testing"

	"synora/internal/cge/fieldtrial"
)

func TestDoctorStateClassification(t *testing.T) {
	manifest := fieldtrial.SessionManifest{Status: fieldtrial.SessionOpen}
	if got := doctorState(manifest, fieldtrial.OperationalTrialStatus{}); got != "healthy" {
		t.Fatalf("healthy state=%s", got)
	}
	warning := fieldtrial.OperationalTrialStatus{ConfigurationDrift: true}
	if got := doctorState(manifest, warning); got != "warning" {
		t.Fatalf("warning state=%s", got)
	}
	degraded := fieldtrial.OperationalTrialStatus{BlockingReasons: []string{"storage"}}
	if got := doctorState(manifest, degraded); got != "degraded" {
		t.Fatalf("degraded state=%s", got)
	}
	manifest.Status = fieldtrial.SessionDegraded
	if got := doctorState(manifest, fieldtrial.OperationalTrialStatus{}); got != "degraded" {
		t.Fatalf("manifest degraded state=%s", got)
	}
}
