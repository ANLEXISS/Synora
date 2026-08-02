package cge

import (
	"context"
	"io"
	"log"
	"reflect"
	"testing"
	"time"
)

func TestShadowWorkflowGoldenHistoricalRegression(t *testing.T) {
	events := []Event{
		{ID: "golden-1", Type: "vision.identity", Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Source: "qualification", Identity: "resident"},
		{ID: "golden-2", Type: "vision.unknown", Timestamp: time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC), Source: "qualification"},
		{ID: "golden-3", Type: "vision.uncertain", Timestamp: time.Date(2026, 1, 1, 0, 2, 0, 0, time.UTC), Source: "qualification"},
	}
	makeConfig := func(root string, enabled bool) ShadowConfig {
		config := DefaultShadowConfig()
		config.Enabled = true
		config.DataDir = root
		config.JournalPath = root + "/historical.ndjson"
		config.InitializeIfMissing = true
		config.JournalID = "golden-regression"
		config.Workflow.Enabled = enabled
		config.Workflow.StoreMode = "memory"
		config.Workflow.MaxProcessingDuration = 2 * time.Second
		return config
	}
	run := func(config ShadowConfig) ([]ObservationResult, MetricsSnapshot, string) {
		engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: events[0].Timestamp}, log.New(io.Discard, "", 0))
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close()
		results := make([]ObservationResult, 0, len(events))
		for _, event := range events {
			result, observeErr := engine.Observe(context.Background(), event)
			if observeErr != nil {
				t.Fatal(observeErr)
			}
			results = append(results, result)
		}
		return results, engine.Metrics(), engine.Status().JournalHeadHash
	}
	withoutResults, withoutMetrics, withoutDigest := run(makeConfig(t.TempDir(), false))
	withResults, withMetrics, withDigest := run(makeConfig(t.TempDir(), true))
	if !reflect.DeepEqual(withoutResults, withResults) {
		t.Fatalf("historical observation results changed\nwithout=%+v\nwith=%+v", withoutResults, withResults)
	}
	if withoutMetrics.AdmissionDisabled != uint64(len(events)) || withMetrics.AdmissionAccepted != uint64(len(events)) {
		t.Fatalf("admission observability did not distinguish disabled and accepted workflow: without=%+v with=%+v", withoutMetrics, withMetrics)
	}
	withoutMetrics.AdmissionDisabled = 0
	withMetrics.AdmissionAccepted = 0
	if !reflect.DeepEqual(withoutMetrics, withMetrics) {
		t.Fatalf("historical metrics changed outside admission observability\nwithout=%+v\nwith=%+v", withoutMetrics, withMetrics)
	}
	if withoutDigest != withDigest {
		t.Fatalf("historical digest changed without=%s with=%s", withoutDigest, withDigest)
	}
}
