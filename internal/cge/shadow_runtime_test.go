package cge

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"synora/internal/cge/chains/generations"
	"synora/internal/cge/durableids"
	"synora/internal/cge/routines"
)

type fixedShadowClock struct{ now time.Time }

func (c fixedShadowClock) Now() time.Time { return c.now }

func quietShadowLogger() Logger { return log.New(io.Discard, "", 0) }

func enabledShadowConfig(root string, initialize bool) ShadowConfig {
	config := DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	config.InitializeIfMissing = initialize
	config.JournalID = "shadow-test-journal"
	return config
}

func shadowEvent(id, eventType string, at time.Time) Event {
	return Event{ID: id, Type: eventType, Source: "vision-worker", Timestamp: at, Identity: "resident-a", DeviceID: "camera-a", NodeID: "entry", ActivationID: "activation-a", TrackID: "track-a", SequenceKey: "sequence-a", ClipID: "clip-a", ClipIndex: 2}
}

func TestAdaptEventUsesOnlyScalarFieldsAndConservativeAllowlist(t *testing.T) {
	at := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	input, err := AdaptEvent(shadowEvent("event-1", "VISION.IDENTITY", at))
	if err != nil || !input.Eligible {
		t.Fatalf("identity adaptation failed: result=%#v err=%v", input, err)
	}
	observation := input.Input.Observation
	if observation.ID != durableids.Protect(durableids.KindObservation, "event-1") || observation.EventType != "vision.identity" || !observation.Timestamp.Equal(at) || observation.EntityID != durableids.Protect(durableids.KindEntity, "resident-a") || observation.ActivationID != durableids.Protect(durableids.KindActivation, "activation-a") || observation.ClipIndex != 2 || observation.SequenceKey != durableids.Protect(durableids.KindSequence, "sequence-a") {
		t.Fatalf("scalar mapping incomplete: %#v", observation)
	}
	motion, err := AdaptEvent(shadowEvent("event-motion", "vision.motion", at))
	if err != nil || motion.Eligible || motion.ReasonCode != ReasonEventTypeNotAllowlisted {
		t.Fatalf("motion should be skipped by default: result=%#v err=%v", motion, err)
	}
	if _, err := AdaptEvent(Event{Type: "vision.identity", Timestamp: at}); err == nil || !errors.Is(err, ErrShadowAdaptation) {
		t.Fatalf("missing ID was not rejected: %v", err)
	}
	if _, err := AdaptEvent(Event{ID: "bad-time", Type: "vision.identity"}); err == nil || !errors.Is(err, ErrShadowAdaptation) {
		t.Fatalf("zero timestamp was not rejected: %v", err)
	}
	if _, err := AdaptEvent(shadowEvent("bad\nid", "vision.identity", at)); err == nil || !errors.Is(err, ErrShadowAdaptation) {
		t.Fatalf("invalid scalar identifier was not rejected: %v", err)
	}
}

func TestShadowDisabledDoesNotTouchConfiguredFilesystem(t *testing.T) {
	root := filepath.Join(t.TempDir(), "not-created")
	config := DefaultShadowConfig()
	config.DataDir = root
	config.JournalPath = filepath.Join(root, "journal.ndjson")
	_, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Now().UTC()}, quietShadowLogger())
	if err != nil {
		t.Fatalf("disabled shadow construction: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled shadow touched filesystem: stat=%v", err)
	}
}

func TestShadowInitializesPersistsAndReloadsWithoutAutomaticSnapshot(t *testing.T) {
	root := t.TempDir()
	clock := fixedShadowClock{now: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)}
	config := enabledShadowConfig(root, true)
	engine, err := NewShadowEngineWithConfig(context.Background(), config, clock, quietShadowLogger())
	if err != nil {
		t.Fatalf("shadow initialization: %v", err)
	}
	shadow := engine
	if _, err := shadow.Observe(context.Background(), shadowEvent("event-1", "vision.identity", clock.now.Add(time.Second))); err != nil {
		t.Fatalf("first shadow observation: %v", err)
	}
	if _, err := shadow.Observe(context.Background(), shadowEvent("event-1", "vision.identity", clock.now.Add(time.Second))); err != nil {
		t.Fatalf("duplicate shadow observation: %v", err)
	}
	metrics := shadow.Metrics()
	if metrics.EventsObserved != 2 || metrics.PlansCreateCandidate != 1 || metrics.AppliedCreateCandidate != 1 || metrics.PlansAlreadyAttached != 1 || metrics.IdempotentNoops != 1 || metrics.LastSuccessAt.IsZero() {
		t.Fatalf("unexpected shadow metrics: %#v", metrics)
	}
	if chains := shadow.coordinator.List(); len(chains) != 1 || len(chains[0].Contributions) != 0 {
		t.Fatalf("shadow observation unexpectedly created contribution: %#v", chains)
	}
	if err := shadow.Close(); err != nil {
		t.Fatalf("shadow close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "snapshots")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("shadow created an automatic snapshot store: %v", err)
	}

	reloadedConfig := enabledShadowConfig(root, false)
	reloaded, err := NewShadowEngineWithConfig(context.Background(), reloadedConfig, clock, quietShadowLogger())
	if err != nil {
		t.Fatalf("shadow journal reload: %v", err)
	}
	if _, err := reloaded.Observe(context.Background(), shadowEvent("event-1", "vision.identity", clock.now.Add(2*time.Second))); err != nil {
		t.Fatalf("duplicate after reload: %v", err)
	}
	if got := reloaded.Metrics(); got.PlansAlreadyAttached != 1 || got.IdempotentNoops != 1 {
		t.Fatalf("reload did not preserve idempotence: %#v", got)
	}
	_ = reloaded.Close()
}

func TestShadowRoutineLearningIsOptionalDurableAndIdempotent(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	config := cognitiveShadowConfig(root)
	config.Context.Enabled = true
	config.Routines.Enabled = true
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: at}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	engine.contextProvider = completeContextProvider(at)
	if _, err := engine.Observe(context.Background(), shadowEvent("routine-shadow-1", "vision.identity", at)); err != nil {
		t.Fatalf("routine shadow observation: %v", err)
	}
	if engine.coordinator.RoutineCount() != 1 || engine.Metrics().RoutineCreated != 1 {
		t.Fatalf("routine was not learned: count=%d metrics=%#v", engine.coordinator.RoutineCount(), engine.Metrics())
	}
	if _, err := engine.Observe(context.Background(), shadowEvent("routine-shadow-1", "vision.identity", at)); err != nil {
		t.Fatalf("routine shadow duplicate: %v", err)
	}
	if engine.Metrics().RoutineOccurrenceIdempotent != 1 {
		t.Fatalf("routine duplicate was not idempotent: %#v", engine.Metrics())
	}
	first := engine.coordinator.ListRoutines()
	if len(first) != 1 || first[0].Status != routines.StatusCandidate {
		t.Fatalf("unexpected routine snapshot: %#v", first)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	reloadedConfig := config
	reloadedConfig.InitializeIfMissing = false
	reloaded, err := NewShadowEngineWithConfig(context.Background(), reloadedConfig, fixedShadowClock{now: at}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if got := reloaded.coordinator.ListRoutines(); !reflect.DeepEqual(first, got) {
		t.Fatalf("routine replay diverged: before=%#v after=%#v", first, got)
	}
}

func TestShadowStartupRequiresExplicitInitializationAndRejectsCorruptManifest(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	config := enabledShadowConfig(root, false)
	if _, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Now().UTC()}, quietShadowLogger()); err == nil || !errors.Is(err, ErrShadowStartup) {
		t.Fatalf("missing journal without initialization was accepted: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed startup created data directory: %v", err)
	}

	if err := os.MkdirAll(root, 0o750); err != nil {
		t.Fatalf("create corrupt manifest root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte("{broken"), 0o640); err != nil {
		t.Fatalf("write corrupt manifest: %v", err)
	}
	if _, err := NewShadowEngineWithConfig(context.Background(), enabledShadowConfig(root, false), fixedShadowClock{now: time.Now().UTC()}, quietShadowLogger()); err == nil || !errors.Is(err, ErrShadowStartup) {
		t.Fatalf("corrupt manifest was silently bypassed: %v", err)
	}
}

func TestShadowStartupLoadsManifestGeneration(t *testing.T) {
	root := t.TempDir()
	clock := fixedShadowClock{now: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)}
	config := enabledShadowConfig(root, true)
	engine, err := NewShadowEngineWithConfig(context.Background(), config, clock, quietShadowLogger())
	if err != nil {
		t.Fatalf("initial shadow: %v", err)
	}
	if _, err := engine.Observe(context.Background(), shadowEvent("manifest-event", "vision.identity", clock.now.Add(time.Second))); err != nil {
		t.Fatalf("seed observation: %v", err)
	}
	store, err := generations.NewStore(root, generations.StoreOptions{})
	if err != nil {
		t.Fatalf("generation store: %v", err)
	}
	if _, err := engine.coordinator.CreateSnapshotGeneration(context.Background(), store, clock.now.Add(time.Minute), config.Actor, "manifest-test"); err != nil {
		t.Fatalf("create explicit generation: %v", err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close initial shadow: %v", err)
	}
	reloaded, err := NewShadowEngineWithConfig(context.Background(), enabledShadowConfig(root, false), clock, quietShadowLogger())
	if err != nil {
		t.Fatalf("manifest startup: %v", err)
	}
	if _, err := reloaded.Observe(context.Background(), shadowEvent("manifest-event", "vision.identity", clock.now.Add(2*time.Minute))); err != nil {
		t.Fatalf("manifest idempotence: %v", err)
	}
	if metrics := reloaded.Metrics(); metrics.PlansAlreadyAttached != 1 || metrics.IdempotentNoops != 1 {
		t.Fatalf("manifest recovery did not restore observation: %#v", metrics)
	}
	_ = reloaded.Close()
}

func TestShadowMetricsAreDefensive(t *testing.T) {
	engine := NewShadowEngine()
	if _, err := engine.Observe(context.Background(), Event{ID: "event", Type: "vision.identity", Timestamp: time.Now().UTC()}); err != nil {
		t.Fatalf("observe: %v", err)
	}
	first := engine.Metrics()
	first.LastErrorCode = "changed"
	if reflect.DeepEqual(first, engine.Metrics()) {
		t.Fatalf("metrics snapshot unexpectedly shared mutable state")
	}
}

func TestShadowLogsNeverContainEventIdentifiers(t *testing.T) {
	root := t.TempDir()
	config := enabledShadowConfig(root, true)
	var logs bytes.Buffer
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)}, log.New(&logs, "", 0))
	if err != nil {
		t.Fatalf("shadow initialization: %v", err)
	}
	defer engine.Close()
	secretID := "sensitive-observation-id"
	if _, err := engine.Observe(context.Background(), Event{ID: secretID, Type: "vision.identity"}); err == nil {
		t.Fatalf("malformed event was accepted")
	}
	if strings.Contains(logs.String(), secretID) {
		t.Fatalf("shadow log leaked event identifier: %q", logs.String())
	}
}
