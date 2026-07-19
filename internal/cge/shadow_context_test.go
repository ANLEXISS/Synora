package cge

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	cgecontext "synora/internal/cge/context"
)

type failingContextProvider struct{ panic bool }

func (p failingContextProvider) Resolve(context.Context, string, time.Time, string) (cgecontext.Frame, error) {
	if p.panic {
		panic("provider test panic")
	}
	return cgecontext.Frame{}, errors.New("provider unavailable")
}

func completeContextProvider(at time.Time) cgecontext.StaticProvider {
	return cgecontext.StaticProvider{Timezone: "Europe/Paris", Occupancy: cgecontext.OccupancyUnknown, HouseMode: cgecontext.HouseModeHome, AllowPartial: false, Topology: cgecontext.TopologySnapshot{Revision: "shadow-topology-1", CapturedAt: at, Nodes: []cgecontext.Node{{ID: "entry", Kind: cgecontext.NodeEntrance, EntryPoint: true}}}}
}

func TestShadowContextDisabledDoesNotResolve(t *testing.T) {
	config := cognitiveShadowConfig(t.TempDir())
	config.Context.Enabled = false
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Now().UTC()}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if _, err := engine.Observe(context.Background(), shadowEvent("context-disabled", "vision.identity", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	metrics := engine.Metrics()
	if metrics.ContextResolutionAttempted != 0 || metrics.ContextResolutionComplete != 0 || metrics.ContextResolutionPartial != 0 {
		t.Fatalf("context was resolved while disabled: %#v", metrics)
	}
}

func TestShadowContextIsAttachedBeforeDurableAssociation(t *testing.T) {
	at := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	config := cognitiveShadowConfig(t.TempDir())
	config.Context.Enabled = true
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: at}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.contextProvider = completeContextProvider(at)
	if _, err := engine.Observe(context.Background(), shadowEvent("context-complete", "vision.identity", at)); err != nil {
		t.Fatal(err)
	}
	chains := engine.coordinator.List()
	if len(chains) != 1 || len(chains[0].Observations) != 1 || chains[0].Observations[0].Context == nil {
		t.Fatalf("context was not durable: %#v", chains)
	}
	frame := chains[0].Observations[0].Context
	if frame.Quality != cgecontext.QualityComplete || frame.TopologyRevision != "shadow-topology-1" || frame.NodeID != "entry" {
		t.Fatalf("unexpected durable frame: %#v", frame)
	}
	metrics := engine.Metrics()
	if metrics.ContextResolutionComplete != 1 || metrics.ContextResolutionPartial != 0 {
		t.Fatalf("unexpected context metrics: %#v", metrics)
	}
	before := chains[0].Observations[0].Context
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
	afterChains := reloaded.coordinator.List()
	if len(afterChains) != 1 || len(afterChains[0].Observations) != 1 || !reflect.DeepEqual(before, afterChains[0].Observations[0].Context) {
		t.Fatalf("context changed across replay: before=%#v after=%#v", before, afterChains)
	}
}

func TestShadowContextProviderFailureIsIsolated(t *testing.T) {
	at := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	config := cognitiveShadowConfig(t.TempDir())
	config.Context.Enabled = true
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: at}, quietShadowLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	engine.contextProvider = failingContextProvider{}
	if _, err := engine.Observe(context.Background(), shadowEvent("context-error", "vision.identity", at)); err != nil {
		t.Fatalf("provider error blocked shadow: %v", err)
	}
	chains := engine.coordinator.List()
	if len(chains) != 1 || chains[0].Observations[0].Context != nil {
		t.Fatalf("provider error changed durable context: %#v", chains)
	}
	metrics := engine.Metrics()
	if metrics.ContextResolutionErrors != 1 || metrics.ContextResolutionMissing != 1 {
		t.Fatalf("unexpected provider error metrics: %#v", metrics)
	}
	engine.contextProvider = failingContextProvider{panic: true}
	if _, err := engine.Observe(context.Background(), shadowEvent("context-panic", "vision.identity", at.Add(time.Second))); err != nil {
		t.Fatalf("provider panic escaped: %v", err)
	}
}
