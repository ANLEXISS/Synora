package demo

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveSessionUsesRealEngineAndReplaysDurableState(t *testing.T) {
	session, err := NewLiveSession(context.Background(), LiveOptions{Seed: 4417, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	initial := session.State()
	if initial.Global.ObservationCount != 0 || initial.Global.ActionsEnabled {
		t.Fatalf("unexpected initial live state: %#v", initial.Global)
	}
	if initial.SyntheticNotice == "" || initial.Topology == nil {
		t.Fatalf("live state is missing isolation metadata or topology: %#v", initial)
	}

	first, err := session.Submit(LiveEventInput{
		EventType: "vision.identity", Identity: "subject-a", NodeID: "entrance",
		HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete",
		SimulatedAt: time.Date(2026, 1, 5, 18, 15, 0, 0, time.UTC), SequenceKey: "resident-a-evening",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Association.Decision != "create_candidate" || !first.Deviation.Attempted {
		t.Fatalf("first real event did not expose engine result: %#v", first)
	}
	if first.Deviation.Status != "insufficient_history" {
		t.Fatalf("first event should expose warm-up state, got %#v", first.Deviation)
	}

	batch, err := session.RunBatch(context.Background(), LiveBatchRequest{
		Input: LiveEventInput{EventType: "vision.identity", Identity: "subject-a", NodeID: "entrance", HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", SimulatedAt: time.Date(2026, 1, 6, 18, 15, 0, 0, time.UTC), SequenceKey: "resident-a-evening"},
		Count: 6, Step: "24h",
	}, nil)
	if err != nil || len(batch) != 6 {
		t.Fatalf("batch did not process one event at a time: count=%d err=%v", len(batch), err)
	}
	state := session.State()
	if state.Global.ObservationCount != 7 || state.Global.RoutineCount == 0 || len(state.Trace) == 0 || len(state.WAL) == 0 {
		t.Fatalf("real mutations are not visible in live state: global=%#v trace=%d wal=%d", state.Global, len(state.Trace), len(state.WAL))
	}
	if state.LastResult == nil || state.LastResult.EventID == "" {
		t.Fatalf("last result was not retained in state")
	}

	ambiguousAt := state.SimulatedAt
	ambiguous, err := session.Submit(LiveEventInput{
		EventType: "vision.identity", Identity: "subject-a", NodeID: "entrance",
		HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete",
		SimulatedAt: ambiguousAt, SequenceKey: "resident-a-evening", PrepareAmbiguity: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ambiguous.Association.Decision != "ambiguous" || ambiguous.Hypothesis.Action == "none" {
		t.Fatalf("ambiguity fixture did not reach the real ambiguous path: %#v", ambiguous)
	}

	before, err := session.Restart()
	if err != nil {
		t.Fatal(err)
	}
	if equal, ok := before["equal"].(bool); !ok || !equal {
		t.Fatalf("durable digest changed on live replay: %#v", before)
	}
	if got := session.State().Global.DeviationStoreCount; got != 0 {
		t.Fatalf("ephemeral deviation store survived restart: %d", got)
	}

	root := filepath.Clean(session.root)
	if filepath.Base(root) == "" || filepath.Dir(root) == "" {
		t.Fatalf("unexpected live root: %q", root)
	}
}

func TestLiveSessionRejectsInvalidInputsAndResets(t *testing.T) {
	session, err := NewLiveSession(context.Background(), LiveOptions{Seed: 19})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if _, err := session.Submit(LiveEventInput{EventType: "not-supported", NodeID: "entrance"}); err == nil {
		t.Fatal("unsupported event type was accepted")
	}
	if _, err := session.Advance(LiveAdvanceRequest{Minutes: 15}); err != nil {
		t.Fatal(err)
	}
	if got := session.State().Global.ObservationCount; got != 0 {
		t.Fatalf("advancing the simulated clock injected an event: %d", got)
	}
	if err := session.Reset(); err != nil {
		t.Fatal(err)
	}
	state := session.State()
	if state.Global.ObservationCount != 0 || len(state.Events) != 0 || state.Global.JournalSequence != 1 {
		t.Fatalf("reset did not create a clean isolated engine: %#v", state)
	}
}

func TestLiveTemporalDeviationUsesFormTimestampBeforeLearning(t *testing.T) {
	session, err := NewLiveSession(context.Background(), LiveOptions{Seed: 7788, Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.LoadBaseline(7); err != nil {
		t.Fatal(err)
	}
	location, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	occurrenceAt := time.Date(2026, 1, 12, 2, 15, 0, 0, location).UTC()
	result, err := session.Submit(LiveEventInput{
		EventType: "vision.identity", Identity: "subject-a", NodeID: "entrance",
		HouseMode: "night", Occupancy: "occupied", ContextQuality: "complete",
		SimulatedAt: occurrenceAt, SequenceKey: "deviation-preset",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Context.ObservedAt != occurrenceAt || result.Context.MinuteOfDay != 135 || result.Context.TimeBucket != 9 {
		t.Fatalf("form timestamp did not reach the context frame: %#v", result.Context)
	}
	if result.Deviation.Status != "evaluated" && result.Deviation.Status != "partial" {
		t.Fatalf("temporal deviation was not evaluated: %#v", result.Deviation)
	}
	if result.Deviation.Routine == nil || result.Deviation.Routine.PatternHouseMode != "home" {
		t.Fatalf("best routine diagnostic is missing historical context: %#v", result.Deviation)
	}
	for _, bin := range result.Deviation.Routine.TemporalBins {
		if bin.Weekday == result.Context.Weekday && bin.TimeBucket == result.Context.TimeBucket {
			t.Fatalf("deviation occurrence unexpectedly shares an exact historical bin: %#v", result.Deviation.Routine.TemporalBins)
		}
	}
	if len(result.Deviation.Factors) == 0 {
		t.Fatalf("deviation factors are missing")
	}
	var temporalFound bool
	for _, factor := range result.Deviation.Factors {
		if factor.Kind == "temporal" {
			temporalFound = true
			if !factor.Available || factor.Score == 0 {
				t.Fatalf("temporal factor did not expose a positive difference: %#v", factor)
			}
		}
	}
	if !temporalFound || result.Deviation.Score == 0 {
		t.Fatalf("expected positive temporal/total deviation: %#v", result.Deviation)
	}
	if result.Learning.OccurrenceCountBefore >= result.Learning.OccurrenceCountAfter {
		t.Fatalf("occurrence was not learned after assessment: %#v", result.Learning)
	}
	baselineIndex, deviationIndex, addedIndex := -1, -1, -1
	for i, step := range result.Trace {
		switch step.Kind {
		case "deviation.baseline_read":
			baselineIndex = i
		case "deviation.evaluated":
			deviationIndex = i
		case "routine.occurrence_added":
			addedIndex = i
		}
	}
	if baselineIndex < 0 || deviationIndex <= baselineIndex || addedIndex <= deviationIndex {
		t.Fatalf("pre-learning trace order is not explicit: baseline=%d deviation=%d added=%d trace=%#v", baselineIndex, deviationIndex, addedIndex, result.Trace)
	}
}

func TestLiveJSONPayloadPreservesSimulatedTemporalFields(t *testing.T) {
	session, err := NewLiveSession(context.Background(), LiveOptions{Seed: 9090})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	location, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Fatal(err)
	}
	encodeInput := func(at time.Time) LiveEventInput {
		payload, marshalErr := json.Marshal(LiveEventInput{EventType: "vision.identity", Identity: "subject-a", NodeID: "entrance", HouseMode: "home", Occupancy: "occupied", ContextQuality: "complete", SimulatedAt: at})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var decoded LiveEventInput
		if unmarshalErr := json.Unmarshal(payload, &decoded); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		return decoded
	}
	firstAt := time.Date(2026, 1, 5, 18, 15, 0, 0, location)
	secondAt := time.Date(2026, 1, 6, 2, 15, 0, 0, location)
	first, err := session.Submit(encodeInput(firstAt))
	if err != nil {
		t.Fatal(err)
	}
	second, err := session.Submit(encodeInput(secondAt))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Context.ObservedAt.Equal(firstAt.UTC()) || !second.Context.ObservedAt.Equal(secondAt.UTC()) {
		t.Fatalf("JSON payload changed ObservedAt: first=%#v second=%#v", first.Context, second.Context)
	}
	if first.Context.TimeBucket == second.Context.TimeBucket || first.Context.MinuteOfDay == second.Context.MinuteOfDay {
		t.Fatalf("18:15 and next-day 02:15 collapsed into the same temporal fields: first=%#v second=%#v", first.Context, second.Context)
	}
}
