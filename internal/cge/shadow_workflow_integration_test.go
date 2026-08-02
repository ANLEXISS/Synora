package cge

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"synora/internal/cge/shadowworkflow"
)

func TestShadowWorkflowDisabledDoesNotStart(t *testing.T) {
	config := DefaultShadowConfig()
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if engine.workflow != nil || engine.WorkflowStatus().State != "disabled" {
		t.Fatalf("workflow unexpectedly active: %+v", engine.WorkflowStatus())
	}
}

func TestShadowWorkflowReceivesRedactedObservationWithoutChangingShadowFlow(t *testing.T) {
	root := t.TempDir()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	config := DefaultShadowConfig()
	config.Enabled = true
	config.DataDir = root
	config.JournalPath = root + "/historical.ndjson"
	config.InitializeIfMissing = true
	config.JournalID = "workflow-integration-test"
	config.Workflow.Enabled = true
	config.Workflow.StoreMode = "memory"
	// This test proves the redacted hand-off, durable episode publication, and
	// historical isolation. The complete advisory pipeline has dedicated
	// integration tests; keeping it out of this boundary test avoids making the
	// assertion depend on the 250ms processing budget under -race.
	config.Workflow.PipelineDepth = shadowworkflow.DepthEpisode
	engine, err := NewShadowEngineWithConfig(context.Background(), config, fixedShadowClock{now: at}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	if _, err := engine.Observe(context.Background(), Event{ID: "workflow-event", Type: "vision.identity", Timestamp: at, Source: "test", Identity: "resident"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && engine.WorkflowStatus().CyclesSucceeded == 0 {
		time.Sleep(time.Millisecond)
	}
	status := engine.WorkflowStatus()
	if status.CyclesSucceeded == 0 || status.CommitsSucceeded == 0 || status.EpisodeCount != 1 {
		t.Fatalf("workflow status=%+v", status)
	}
	if engine.Metrics().EventsObserved != 1 {
		t.Fatalf("historical metrics changed unexpectedly: %+v", engine.Metrics())
	}
}
