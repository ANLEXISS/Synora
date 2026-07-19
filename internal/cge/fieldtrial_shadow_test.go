package cge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"synora/internal/cge/fieldtrial"
)

func TestFieldTrialRecorderIsSecondaryAndExpunged(t *testing.T) {
	root := t.TempDir()
	config := enabledShadowConfig(t.TempDir(), true)
	config.FieldTrial = fieldtrial.DefaultConfig()
	config.FieldTrial.Enabled = true
	config.FieldTrial.RootDir = root
	config.FieldTrial.SessionID = "cge-trial-shadow"
	config.Cognitive.Enabled = true
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC)}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Observe(context.Background(), shadowEvent("event-raw", "vision.identity", time.Date(2026, 7, 19, 10, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	events, _, manifest, err := fieldtrial.ReadEvents(context.Background(), filepath.Join(root, "cge-trial-shadow"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.EventCount != 1 || len(events) != 1 {
		t.Fatalf("manifest=%+v events=%d", manifest, len(events))
	}
	if events[0].EventRef == "event-raw" || events[0].ChainRef == "" {
		t.Fatalf("event was not expunged: %+v", events[0])
	}
	if engine.coordinator.Status().State == "degraded" {
		t.Fatal("telemetry degraded coordinator")
	}
}

func TestFieldTrialFailureDoesNotPreventShadow(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := enabledShadowConfig(t.TempDir(), true)
	config.FieldTrial = fieldtrial.DefaultConfig()
	config.FieldTrial.Enabled = true
	config.FieldTrial.RootDir = root
	config.FieldTrial.SessionID = "cge-trial-failure"
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Now().UTC()}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if _, err := engine.Observe(context.Background(), shadowEvent("event-still-works", "vision.identity", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if string(engine.Status().State) == "degraded" {
		t.Fatal("recorder failure changed durable state")
	}
}
