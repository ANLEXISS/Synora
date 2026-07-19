package cge

import (
	"context"
	"fmt"
	"testing"
	"time"

	"synora/internal/cge/deviation"
)

func BenchmarkShadowProcessObservationDeviationDisabled(b *testing.B) {
	config := enabledShadowConfig(b.TempDir(), true)
	clock := fixedShadowClock{now: time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)}
	engine, err := NewShadowEngineWithConfig(context.Background(), config, clock, quietShadowLogger())
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Observe(context.Background(), shadowEvent(fmt.Sprintf("bench-disabled-%d", i), "vision.identity", clock.now)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShadowProcessObservationDeviationInsufficientHistory(b *testing.B) {
	config := enabledShadowConfig(b.TempDir(), true)
	config.Deviation.Enabled = true
	config.Deviation.RecentAssessmentLimit = 64
	clock := fixedShadowClock{now: time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)}
	engine, err := NewShadowEngineWithConfig(context.Background(), config, clock, quietShadowLogger())
	if err != nil {
		b.Fatal(err)
	}
	defer engine.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Observe(context.Background(), shadowEvent(fmt.Sprintf("bench-insufficient-%d", i), "vision.identity", clock.now)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecentDeviationStoreAdd(b *testing.B) {
	store, err := NewRecentDeviationStore(256)
	if err != nil {
		b.Fatal(err)
	}
	assessment, err := deviation.EvaluateOccurrence(testDeviationOccurrence(b), nil, atTestTime(), deviation.DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Add(assessment); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRecentDeviationStoreList(b *testing.B) {
	store, err := NewRecentDeviationStore(256)
	if err != nil {
		b.Fatal(err)
	}
	assessment, err := deviation.EvaluateOccurrence(testDeviationOccurrence(b), nil, atTestTime(), deviation.DefaultPolicy())
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 256; i++ {
		if err := store.Add(assessment); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = store.List(32)
	}
}
